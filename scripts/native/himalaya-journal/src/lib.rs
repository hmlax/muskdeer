#[path = "../../../../deer/third_party/himalaya/order/maker_journal.rs"]
pub mod maker_journal;

#[cfg(test)]
mod tests {
    use super::maker_journal::{Command, Error, Journal};
    use sqlx::mysql::MySqlPoolOptions;

    fn command(client: &str, order: u64) -> Command {
        Command {
            user_id: 10001,
            fund_id: 10001,
            symbol: "BTCUSDT".into(),
            client_id: client.into(),
            order_id: order,
            business: b"exact immutable business fields v1".to_vec(),
            authority: b"initial independent native proofs".to_vec(),
            incarnation: "order-service.first".into(),
        }
    }

    #[tokio::test(flavor = "multi_thread", worker_threads = 4)]
    async fn mysql_identity_is_unique_durable_and_never_replays_an_uncertain_claim() {
        // The runner creates and later drops only a fresh logical test database
        // on the already running infra MySQL. Missing infra is a test failure.
        let url = std::env::var("HIMALAYA_MAKER_TEST_DATABASE_URL").expect("run through run.py");
        let pool = MySqlPoolOptions::new()
            .max_connections(12)
            .connect(&url)
            .await
            .unwrap();
        let journal = Journal::new(pool.clone());
        journal.verify_schema().await.unwrap();

        let mut contenders = Vec::new();
        for id in 100..132 {
            let journal = journal.clone();
            contenders.push(tokio::spawn(async move {
                journal.reserve(command("concurrent", id)).await.unwrap()
            }));
        }
        let mut granted = Vec::new();
        let mut order_id = 0;
        for contender in contenders {
            let reservation = contender.await.unwrap();
            if order_id == 0 {
                order_id = reservation.record.order_id;
            }
            assert_eq!(reservation.record.order_id, order_id);
            if reservation.fresh {
                granted.push(reservation.record.order_id);
            }
        }
        assert_eq!(granted, vec![order_id], "exactly one process may dispatch");
        let mut conflict = command("concurrent", 999);
        conflict.business.push(b'!');
        assert!(matches!(
            journal.reserve(conflict).await,
            Err(Error::Conflict)
        ));
        let stored = journal
            .by_client(10001, 10001, "concurrent")
            .await
            .unwrap()
            .unwrap();
        assert_eq!(stored.business, command("concurrent", 0).business);
        assert!(stored.response.is_none());

        // Simulate a successful DB commit followed by process/reply loss before
        // the caller can learn whether native submission happened.
        assert!(
            journal
                .reserve(command("reply-lost", 201))
                .await
                .unwrap()
                .fresh
        );
        drop(journal);
        pool.close().await;
        let pool = MySqlPoolOptions::new()
            .max_connections(4)
            .connect(&url)
            .await
            .unwrap();
        let journal = Journal::new(pool.clone());
        let recovery = journal.reserve(command("reply-lost", 202)).await.unwrap();
        assert!(
            !recovery.fresh,
            "restart must query, never blindly send again"
        );
        assert_eq!(recovery.record.order_id, 201);
        assert!(recovery.record.response.is_none());

        journal
            .observe(201, b"confirmed live order", false)
            .await
            .unwrap();
        assert_eq!(
            journal
                .by_order(10001, 10001, 201)
                .await
                .unwrap()
                .unwrap()
                .response
                .as_deref(),
            Some(b"confirmed live order".as_slice())
        );
        journal
            .observe(201, b"confirmed canceled after partial fill", true)
            .await
            .unwrap();
        assert!(matches!(
            journal.observe(201, b"late live reply", false).await,
            Err(Error::Conflict)
        ));
        assert!(journal.by_order(10002, 10002, 201).await.unwrap().is_none());
        let active = journal.active_page(0, 256).await.unwrap();
        assert!(active.iter().all(|record| record.order_id != 201));
        assert_eq!(active.len(), 1);
        sqlx::query("ALTER TABLE himalaya_maker_command DROP INDEX maker_command_identity")
            .execute(&pool)
            .await
            .unwrap();
        assert!(
            matches!(journal.verify_schema().await, Err(Error::MissingSchema)),
            "a version label without the unique business key cannot enable writes"
        );
    }
}
