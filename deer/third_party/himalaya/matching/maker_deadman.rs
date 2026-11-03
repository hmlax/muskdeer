//! Matching owns expiry cancellation even when the order gateway is unavailable.
use std::collections::BTreeMap;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use std::time::Duration;
use tokio::sync::oneshot;
use tonic::Status;

use crate::{engine::EngineState, proto::CancelOrderRequest};

const PAGE_SIZE: usize = 128;
pub(crate) const MAX_RESTING_PER_ACCOUNT: usize = 64;

pub struct Deadman {
    stop: oneshot::Sender<()>,
    task: tokio::task::JoinHandle<()>,
}

struct Readiness(Arc<AtomicBool>);
impl Drop for Readiness {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

impl Deadman {
    pub fn start(engine: EngineState) -> Option<Self> {
        engine.maker_authority.as_ref()?;
        let (stop, mut stopped) = oneshot::channel();
        let task = tokio::spawn(async move {
            let _readiness = Readiness(engine.maker_deadman_ready.clone());
            engine.maker_deadman_ready.store(true, Ordering::Release);
            loop {
                let force = tokio::select! {
                    _ = &mut stopped => true,
                    _ = tokio::time::sleep(Duration::from_millis(200)) => false,
                };
                if force || engine.order_admission_ready() {
                    let owned = engine.clone();
                    let result =
                        nats_subjects::run_blocking(move || sweep(&owned, force, false)).await;
                    if !matches!(result, Ok(Ok(_))) {
                        // A running book cannot keep accepting producer risk
                        // after losing its cancellation owner. Restart recovery
                        // will withdraw unowned quotes before admission opens.
                        tracing::error!("native maker cancellation owner failed; closing matching");
                        std::process::abort();
                    }
                }
                if force {
                    break;
                }
            }
        });
        Some(Self { stop, task })
    }

    /// Called after ordinary queue drain, while durable publishers still exist.
    pub async fn shutdown(self) -> std::io::Result<()> {
        let _ = self.stop.send(());
        self.task.await.map_err(std::io::Error::other)
    }
}

pub(crate) fn sweep(engine: &EngineState, force: bool, recovery: bool) -> Result<usize, Status> {
    let Some(registry) = engine.maker_authority.as_ref() else {
        return Ok(0);
    };
    let mut after: Option<(String, u64)> = None;
    let mut canceled = 0;
    loop {
        // Bound memory even if another native participant has a large history.
        let mut page = BTreeMap::new();
        for entry in engine.order_outcomes.iter() {
            let req = &entry.request;
            if entry.terminal
                || !matches!(
                    (req.user_id, req.fund_id, req.symbol.as_str()),
                    (10001, 10001, "BTCUSDT") | (10002, 10002, "ETHUSDT")
                )
            {
                continue;
            }
            let key = entry.key().clone();
            if after.as_ref().is_some_and(|last| &key <= last) {
                continue;
            }
            page.insert(key, ());
            if page.len() > PAGE_SIZE {
                page.pop_last();
            }
        }
        if page.is_empty() {
            break;
        }
        after = page.last_key_value().map(|(key, ())| key.clone());
        for ((symbol, order_id), ()) in page {
            // Do not retain a DashMap guard while acquiring authority or the
            // symbol owner: final execution takes those locks in the other order.
            let proof = engine
                .maker_orders
                .get(&(symbol.clone(), order_id))
                .map(|entry| entry.value().clone());
            let active = !force
                && proof.is_some_and(|proof| {
                    registry
                        .authorize(&proof.execution, || engine.maker_now())
                        .is_ok()
                        && registry
                            .authorize(&proof.strategy, || engine.maker_now())
                            .is_ok()
                });
            if active {
                continue;
            }
            let request = CancelOrderRequest {
                order_id,
                symbol: symbol.clone(),
                ..Default::default()
            };
            let result = if recovery {
                crate::place_pipeline::cancel_recovered(engine.clone(), request)
            } else {
                crate::place_pipeline::execute_cancel(engine.clone(), request)
            }?;
            if result.outcome_unknown {
                return Err(Status::unavailable(
                    "owned quote cancellation has no outcome evidence",
                ));
            }
            let terminal = engine
                .order_outcomes
                .get(&(symbol.clone(), order_id))
                .is_some_and(|outcome| outcome.terminal);
            if !terminal {
                return Err(Status::unavailable(
                    "owned quote withdrawal did not reach a terminal outcome",
                ));
            }
            engine.maker_orders.remove(&(symbol, order_id));
            canceled += 1;
        }
    }
    engine.maker_orders.retain(|key, _| {
        engine
            .order_outcomes
            .get(key)
            .is_some_and(|outcome| !outcome.terminal)
    });
    Ok(canceled)
}
