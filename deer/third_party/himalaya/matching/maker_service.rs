//! Authenticated test-funds producer authority at the native matching owner.
use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::{
    engine::EngineState,
    maker_fence::{self, Kind, Registry, Scope},
    maker_matching_proto::{self as wire, matching_authority_server::MatchingAuthority},
    maker_proto as api,
    service::MatchingEngineService,
};

pub(crate) fn adopted_account(user: i64, fund: i64) -> bool {
    matches!(if fund == 0 { user } else { fund }, 10001 | 10002)
}

pub(crate) struct SystemAuthorization {
    pub user_id: i64,
    pub fund_id: i64,
    pub symbol: String,
    pub order_id: u64,
    pub deadline: u64,
}

impl SystemAuthorization {
    pub(crate) fn valid(&self, req: &crate::proto::PlaceOrderRequest, now: u64) -> bool {
        use proto_wire::matching::{order_kind, time_in_force};
        self.user_id == req.user_id
            && self.fund_id == req.fund_id
            && self.symbol == req.symbol
            && self.order_id == req.order_id
            && self.order_id != 0
            && now < self.deadline
            && req.time_in_force == time_in_force::IOC
            && matches!(req.order_kind, order_kind::LIMIT | order_kind::MARKET)
            && matches!(
                (req.user_id, req.fund_id, req.symbol.as_str()),
                (10001, 10001, "BTCUSDT") | (10002, 10002, "ETHUSDT")
            )
    }
}

#[derive(Clone)]
pub struct MatchingAuthorityService {
    matching: MatchingEngineService,
}

impl MatchingAuthorityService {
    pub fn new(matching: MatchingEngineService) -> Self {
        Self { matching }
    }

    fn authority(&self) -> Result<(EngineState, Arc<Registry>), Status> {
        let engine = self.matching.engine.clone();
        let registry = engine.maker_authority.clone().ok_or_else(|| {
            Status::failed_precondition("development maker authority is disabled")
        })?;
        if !registry.durable() || !engine.order_admission_ready() {
            return Err(Status::unavailable(
                "development maker authority is not ready",
            ));
        }
        Ok((engine, registry))
    }
}

pub(crate) fn authority_status(error: maker_fence::Error) -> Status {
    crate::place_pipeline::not_admitted(match error {
        maker_fence::Error::Invalid => {
            Status::invalid_argument("invalid maker authority scope or command")
        }
        maker_fence::Error::Held => Status::already_exists("maker scope has an active owner"),
        maker_fence::Error::Lost => {
            Status::failed_precondition("maker authority or command expired")
        }
        maker_fence::Error::Capacity => {
            Status::resource_exhausted("maker authority capacity reached")
        }
        maker_fence::Error::Exhausted | maker_fence::Error::Durability => {
            Status::unavailable("maker authority cannot prove durable ownership")
        }
    })
}

fn scope(value: Option<api::Scope>) -> Result<Scope, Status> {
    let value = value.ok_or_else(|| Status::invalid_argument("missing maker scope"))?;
    let kind = match api::LeaseKind::try_from(value.kind) {
        Ok(api::LeaseKind::Execution) => Kind::Execution,
        Ok(api::LeaseKind::Strategy) => Kind::Strategy,
        _ => return Err(Status::invalid_argument("invalid maker role")),
    };
    let scope = Scope {
        kind,
        user_id: value.user_id,
        fund_id: value.fund_id,
        symbol: value.symbol,
    };
    let allowed_symbol = match (scope.user_id, scope.fund_id) {
        (10001, 10001) => "BTCUSDT",
        (10002, 10002) => "ETHUSDT",
        _ => {
            return Err(Status::permission_denied(
                "account is outside the development maker profile",
            ));
        }
    };
    if !scope.valid() || kind == Kind::Strategy && scope.symbol != allowed_symbol {
        return Err(Status::permission_denied(
            "market is outside the development maker profile",
        ));
    }
    Ok(scope)
}

fn proof(value: Option<api::Proof>) -> Result<maker_fence::Proof, Status> {
    let value = value.ok_or_else(|| Status::invalid_argument("missing maker proof"))?;
    Ok(maker_fence::Proof {
        scope: scope(value.scope)?,
        owner: value.owner,
        epoch: value.epoch,
    })
}

fn lease(value: maker_fence::Lease, engine: &EngineState) -> api::Lease {
    let p = value.proof;
    api::Lease {
        proof: Some(api::Proof {
            scope: Some(api::Scope {
                kind: match p.scope.kind {
                    Kind::Execution => api::LeaseKind::Execution,
                    Kind::Strategy => api::LeaseKind::Strategy,
                } as i32,
                user_id: p.scope.user_id,
                fund_id: p.scope.fund_id,
                symbol: p.scope.symbol,
            }),
            owner: p.owner,
            epoch: p.epoch,
        }),
        remaining_ms: value.expires_at.saturating_sub(engine.maker_now()),
        incarnation: engine.engine_epoch.as_ref().clone(),
    }
}

#[tonic::async_trait]
impl MatchingAuthority for MatchingAuthorityService {
    async fn get_status(
        &self,
        _: Request<api::CapabilitiesRequest>,
    ) -> Result<Response<wire::Status>, Status> {
        let enabled = self.matching.engine.maker_authority.is_some();
        let durable = self
            .matching
            .engine
            .maker_authority
            .as_ref()
            .is_some_and(|r| r.durable());
        Ok(Response::new(wire::Status {
            development_test_funds: enabled,
            durable_authority: durable && self.matching.engine.order_admission_ready(),
            incarnation: self.matching.engine.engine_epoch.as_ref().clone(),
            owner_expiry_cancel: self
                .matching
                .engine
                .maker_deadman_ready
                .load(std::sync::atomic::Ordering::Acquire),
        }))
    }

    async fn claim_lease(
        &self,
        request: Request<api::ClaimLeaseRequest>,
    ) -> Result<Response<api::Lease>, Status> {
        let (engine, registry) = self.authority()?;
        let request = request.into_inner();
        let scope = scope(request.scope)?;
        let value = nats_subjects::run_blocking(move || {
            registry
                .claim(scope, &request.owner, request.ttl_ms, || engine.maker_now())
                .map(|value| lease(value, &engine))
                .map_err(authority_status)
        })
        .await
        .map_err(|_| Status::internal("maker grant worker failed"))??;
        Ok(Response::new(value))
    }

    async fn renew_lease(
        &self,
        request: Request<api::RenewLeaseRequest>,
    ) -> Result<Response<api::Lease>, Status> {
        let (engine, registry) = self.authority()?;
        let request = request.into_inner();
        let proof = proof(request.proof)?;
        let value = nats_subjects::run_blocking(move || {
            registry
                .renew(&proof, request.ttl_ms, || engine.maker_now())
                .map(|value| lease(value, &engine))
                .map_err(authority_status)
        })
        .await
        .map_err(|_| Status::internal("maker renewal worker failed"))??;
        Ok(Response::new(value))
    }

    async fn release_lease(
        &self,
        request: Request<api::ReleaseLeaseRequest>,
    ) -> Result<Response<api::ReleaseLeaseResponse>, Status> {
        let (engine, registry) = self.authority()?;
        let proof = proof(request.into_inner().proof)?;
        nats_subjects::run_blocking(move || {
            registry
                .release(&proof, || engine.maker_now())
                .map_err(authority_status)
        })
        .await
        .map_err(|_| Status::internal("maker release worker failed"))??;
        Ok(Response::new(api::ReleaseLeaseResponse { released: true }))
    }

    async fn check(
        &self,
        request: Request<wire::CheckRequest>,
    ) -> Result<Response<wire::CheckResponse>, Status> {
        let (engine, registry) = self.authority()?;
        let request = request.into_inner();
        let execution = proof(request.execution)?;
        let strategy = proof(request.strategy)?;
        match registry.admit(execution, strategy, 1, || engine.maker_now()) {
            Ok(_) => Ok(Response::new(wire::CheckResponse { active: true })),
            Err(maker_fence::Error::Lost) => {
                Ok(Response::new(wire::CheckResponse { active: false }))
            }
            Err(error) => Err(authority_status(error)),
        }
    }

    async fn system_reduce(
        &self,
        request: Request<wire::SystemReduceRequest>,
    ) -> Result<Response<crate::proto::PlaceOrderResponse>, Status> {
        app_config::auth::require_admin(&request).map_err(crate::place_pipeline::not_admitted)?;
        let (engine, _) = self.authority()?;
        let request = request.into_inner();
        if request.episode_id.is_empty()
            || request.episode_id.len() > 128
            || !request
                .episode_id
                .bytes()
                .all(|c| c.is_ascii_alphanumeric() || b"._-:".contains(&c))
            || request.maximum_queue_age_ms == 0
            || request.maximum_queue_age_ms > 2_000
        {
            return Err(crate::place_pipeline::not_admitted(
                Status::invalid_argument("invalid native liquidation admission"),
            ));
        }
        let order = request.order.ok_or_else(|| {
            crate::place_pipeline::not_admitted(Status::invalid_argument(
                "missing native liquidation order",
            ))
        })?;
        let authority = SystemAuthorization {
            user_id: order.user_id,
            fund_id: order.fund_id,
            symbol: order.symbol.clone(),
            order_id: order.order_id,
            deadline: engine
                .maker_now()
                .saturating_add(u64::from(request.maximum_queue_age_ms)),
        };
        if !authority.valid(&order, engine.maker_now()) {
            return Err(crate::place_pipeline::not_admitted(
                Status::invalid_argument("native reduction must be an adopted account IOC"),
            ));
        }
        self.matching
            .system_reduce(order, authority)
            .await
            .map(Response::new)
    }

    async fn place(
        &self,
        request: Request<wire::PlaceRequest>,
    ) -> Result<Response<crate::proto::PlaceOrderResponse>, Status> {
        let (engine, registry) = self.authority()?;
        let request = request.into_inner();
        let execution = proof(request.execution)?;
        let strategy = proof(request.strategy)?;
        let order = request
            .order
            .ok_or_else(|| Status::invalid_argument("missing maker order"))?;
        if order.symbol != strategy.scope.symbol
            || order.user_id != strategy.scope.user_id
            || order.fund_id != strategy.scope.fund_id
            || order.order_kind != proto_wire::matching::order_kind::POST_ONLY
            || order.time_in_force != proto_wire::matching::time_in_force::GTC
        {
            return Err(Status::invalid_argument(
                "maker order does not match its authority or post-only contract",
            ));
        }
        let authority = registry
            .admit(
                execution,
                strategy,
                u64::from(request.maximum_queue_age_ms),
                || engine.maker_now(),
            )
            .map_err(authority_status)?;
        self.matching
            .maker_place(order, authority)
            .await
            .map(Response::new)
    }
}
