//! Invariants at the actual order-book effect boundary, not a client-side gate.
use std::sync::Arc;

use crate::{
    engine::EngineState,
    maker_fence::{Authorization, Kind, Registry, Scope},
    place_pipeline::execute_place_authorized,
    proto::PlaceOrderRequest,
};
use proto_wire::matching::{order_kind, side, time_in_force};

pub(crate) fn setup() -> (EngineState, Authorization) {
    let mut engine = EngineState::new(None, None);
    let registry = Arc::new(Registry::load(None).unwrap());
    engine.maker_authority = Some(registry.clone());
    let execution = registry
        .claim(
            Scope {
                kind: Kind::Execution,
                user_id: 10001,
                fund_id: 10001,
                symbol: String::new(),
            },
            "execution.first",
            30_000,
            || engine.maker_now(),
        )
        .unwrap();
    let strategy = registry
        .claim(
            Scope {
                kind: Kind::Strategy,
                user_id: 10001,
                fund_id: 10001,
                symbol: "BTCUSDT".into(),
            },
            "strategy.first",
            30_000,
            || engine.maker_now(),
        )
        .unwrap();
    let authorization = registry
        .admit(execution.proof, strategy.proof, 2_000, || {
            engine.maker_now()
        })
        .unwrap();
    (engine, authorization)
}

fn order(id: u64) -> PlaceOrderRequest {
    PlaceOrderRequest {
        order_id: id,
        symbol: "BTCUSDT".into(),
        side: side::BUY,
        order_kind: order_kind::POST_ONLY,
        price: 10_000,
        quantity: 5,
        time_in_force: time_in_force::GTC,
        user_id: 10001,
        fund_id: 10001,
        ..Default::default()
    }
}

#[test]
fn authority_handover_rejects_old_request_before_any_orderbook_effect() {
    let (engine, old) = setup();
    let registry = engine.maker_authority.as_ref().unwrap();
    registry
        .release(&old.strategy, || engine.maker_now())
        .unwrap();
    let next = registry
        .claim(
            old.strategy.scope.clone(),
            "strategy.second",
            30_000,
            || engine.maker_now(),
        )
        .unwrap();
    assert!(next.proof.epoch > old.strategy.epoch);
    let error = execute_place_authorized(engine.clone(), order(401), Some(old)).unwrap_err();
    assert_eq!(error.code(), tonic::Code::FailedPrecondition);
    assert!(engine.order_outcomes.is_empty());
    assert!(engine.books.is_empty());
}

#[test]
fn authority_cannot_be_reused_for_another_fund_or_symbol() {
    for change in [0, 1] {
        let (engine, authority) = setup();
        let mut req = order(402);
        if change == 0 {
            req.fund_id += 1;
        } else {
            req.symbol = "ETHUSDT".into();
        }
        assert!(execute_place_authorized(engine.clone(), req, Some(authority)).is_err());
        assert!(engine.order_outcomes.is_empty());
        assert!(engine.books.is_empty());
    }
}

#[test]
fn valid_authority_writes_once_and_identical_order_identity_does_not_duplicate() {
    let (engine, authority) = setup();
    let first =
        execute_place_authorized(engine.clone(), order(403), Some(authority.clone())).unwrap();
    assert_eq!(first.remaining_quantity, 5);
    let duplicate = execute_place_authorized(engine.clone(), order(403), Some(authority)).unwrap();
    assert_eq!(duplicate, first);
    assert_eq!(engine.order_outcomes.len(), 1);
    let book = engine.get_or_create_entry("BTCUSDT");
    assert_eq!(
        book.book.create_snapshot(20).unwrap().bids[0]
            .visible_quantity()
            .as_u64(),
        5
    );
}
