use crate::{maker_execution_tests::setup, place_pipeline, proto::*};
use proto_wire::matching::{order_kind, side, time_in_force};

fn order(id: u64) -> PlaceOrderRequest {
    PlaceOrderRequest {
        order_id: id,
        symbol: "BTCUSDT".into(),
        user_id: 10001,
        fund_id: 10001,
        side: side::BUY,
        order_kind: order_kind::POST_ONLY,
        time_in_force: time_in_force::GTC,
        price: 10_000,
        quantity: 5,
        ..Default::default()
    }
}

#[test]
fn target_expiry_cancels_only_remaining_quote_and_keeps_partial_execution_evidence() {
    let (engine, proof) = setup();
    place_pipeline::execute_place_authorized(engine.clone(), order(501), Some(proof.clone()))
        .unwrap();
    let mut taker = order(502);
    taker.user_id = 3;
    taker.fund_id = 3;
    taker.side = side::SELL;
    taker.order_kind = order_kind::LIMIT;
    taker.time_in_force = time_in_force::IOC;
    taker.quantity = 2;
    place_pipeline::execute_place(engine.clone(), taker).unwrap();
    assert_eq!(
        crate::maker_deadman::sweep(&engine, false, false).unwrap(),
        0
    );
    engine
        .maker_authority
        .as_ref()
        .unwrap()
        .release(&proof.strategy, || engine.maker_now())
        .unwrap();
    assert_eq!(
        crate::maker_deadman::sweep(&engine, false, false).unwrap(),
        1
    );
    let outcome = engine.order_outcomes.get(&("BTCUSDT".into(), 501)).unwrap();
    assert!(outcome.terminal);
    assert_eq!(outcome.result.executed_quantity, 2);
    assert_eq!(outcome.result.status, "CANCELLED");
    assert!(
        engine
            .get_or_create_entry("BTCUSDT")
            .book
            .create_snapshot(20)
            .unwrap()
            .bids
            .is_empty()
    );
}

#[test]
fn restart_recovery_withdraws_unowned_quotes_before_reopening_admission() {
    let (engine, _) = setup();
    engine.require_order_recovery();
    let session = engine.begin_order_recovery();
    for (id, user) in [(503, 10001), (504, 3)] {
        let mut req = order(id);
        req.user_id = user;
        req.fund_id = user;
        crate::recovery::restore_order(
            engine.clone(),
            RestoreOrderRequest {
                session: Some(session.clone()),
                order: Some(req),
                executed_quantity: 2,
                created_at: 1,
                terminal_executed_quantity: None,
            },
        )
        .unwrap();
    }
    assert!(!engine.order_admission_ready());
    engine.complete_order_recovery(&session).unwrap();
    assert!(engine.order_admission_ready());
    assert!(
        engine
            .order_outcomes
            .get(&("BTCUSDT".into(), 503))
            .unwrap()
            .terminal
    );
    assert!(
        !engine
            .order_outcomes
            .get(&("BTCUSDT".into(), 504))
            .unwrap()
            .terminal
    );
    assert_eq!(
        engine
            .get_or_create_entry("BTCUSDT")
            .book
            .create_snapshot(20)
            .unwrap()
            .bids[0]
            .visible_quantity()
            .as_u64(),
        3
    );
}

#[tokio::test]
async fn owned_shutdown_withdraws_even_live_proofs_without_flattening() {
    let (engine, proof) = setup();
    place_pipeline::execute_place_authorized(engine.clone(), order(505), Some(proof)).unwrap();
    let worker = crate::maker_deadman::Deadman::start(engine.clone()).unwrap();
    worker.shutdown().await.unwrap();
    assert!(
        engine
            .order_outcomes
            .get(&("BTCUSDT".into(), 505))
            .unwrap()
            .terminal
    );
    assert!(
        !engine
            .maker_deadman_ready
            .load(std::sync::atomic::Ordering::Acquire)
    );
}

#[test]
fn missing_target_cancellation_owner_closes_new_risk_and_resting_quotes_are_bounded() {
    let (engine, proof) = setup();
    engine
        .maker_deadman_ready
        .store(false, std::sync::atomic::Ordering::Release);
    assert!(
        place_pipeline::execute_place_authorized(engine.clone(), order(506), Some(proof.clone()))
            .is_err()
    );
    assert!(engine.books.is_empty());
    engine
        .maker_deadman_ready
        .store(true, std::sync::atomic::Ordering::Release);
    for id in 600..664 {
        let current = engine
            .maker_authority
            .as_ref()
            .unwrap()
            .admit(
                proof.execution.clone(),
                proof.strategy.clone(),
                2_000,
                || engine.maker_now(),
            )
            .unwrap();
        place_pipeline::execute_place_authorized(engine.clone(), order(id), Some(current)).unwrap();
    }
    let current = engine
        .maker_authority
        .as_ref()
        .unwrap()
        .admit(proof.execution, proof.strategy, 2_000, || {
            engine.maker_now()
        })
        .unwrap();
    assert_eq!(
        place_pipeline::execute_place_authorized(engine.clone(), order(664), Some(current))
            .unwrap_err()
            .code(),
        tonic::Code::ResourceExhausted
    );
    assert_eq!(engine.maker_orders.len(), 64);
}
