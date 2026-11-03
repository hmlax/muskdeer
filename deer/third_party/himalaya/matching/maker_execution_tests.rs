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

#[test]
fn adopted_account_cannot_bypass_final_fence_via_legacy_place() {
    let (engine, _) = setup();
    let result = crate::place_pipeline::execute_place(engine.clone(), order(407));
    assert!(
        result.is_err(),
        "legacy RPC bypassed the adopted account's final fence"
    );
    assert!(engine.order_outcomes.is_empty());
    assert!(engine.books.is_empty());
}

#[test]
fn native_reduction_revokes_producers_and_preserves_its_ioc_execution() {
    let (engine, old) = setup();
    let mut counterparty = order(408);
    counterparty.user_id = 3;
    counterparty.fund_id = 3;
    counterparty.side = side::SELL;
    crate::place_pipeline::execute_place(engine.clone(), counterparty).unwrap();
    let mut reduce = order(409);
    reduce.order_kind = order_kind::LIMIT;
    reduce.time_in_force = time_in_force::IOC;
    let system = crate::maker_service::SystemAuthorization {
        user_id: 10001,
        fund_id: 10001,
        symbol: "BTCUSDT".into(),
        order_id: 409,
        deadline: engine.maker_now() + 2_000,
    };
    let response =
        crate::place_pipeline::execute_place_guarded(engine.clone(), reduce, None, Some(system))
            .unwrap();
    assert_eq!(response.executed_quantity, 5);
    assert!(execute_place_authorized(engine.clone(), order(410), Some(old.clone())).is_err());
    let registry = engine.maker_authority.as_ref().unwrap();
    assert_eq!(
        registry.authorize(&old.execution, || engine.maker_now()),
        Err(crate::maker_fence::Error::Lost)
    );
    assert_eq!(
        registry.authorize(&old.strategy, || engine.maker_now()),
        Err(crate::maker_fence::Error::Lost)
    );
}

#[tokio::test]
async fn ordinary_service_identity_cannot_enter_native_reduction_lane() {
    use crate::maker_matching_proto::matching_authority_server::MatchingAuthority;
    let engine = EngineState::new(None, None);
    let markets = Arc::new(crate::market_registry::MarketRegistry::from_markets(
        Vec::new(),
    ));
    let svc = crate::maker_service::MatchingAuthorityService::new(
        crate::service::MatchingEngineService::new(engine.clone(), markets),
    );
    let mut request = tonic::Request::new(crate::maker_matching_proto::SystemReduceRequest {
        order: Some(order(411)),
        episode_id: "test-liquidation".into(),
        maximum_queue_age_ms: 1_000,
    });
    request
        .extensions_mut()
        .insert(app_config::auth::Identity { admin: false });
    assert_eq!(
        svc.system_reduce(request).await.unwrap_err().code(),
        tonic::Code::PermissionDenied
    );
    assert!(engine.books.is_empty());
}
