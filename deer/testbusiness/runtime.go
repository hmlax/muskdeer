// Package testbusiness retains combined assembly only for legacy regression commands.
package testbusiness

import (
	"context"
	"slices"

	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	strategyv1 "github.com/hmlax/muskdeer/deer/proto/rpc/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/adminactor"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	executionbusiness "github.com/hmlax/muskdeer/execution/business"
	marketbusiness "github.com/hmlax/muskdeer/market/business"
	"github.com/hmlax/muskdeer/strategy/delivery/strategygrpc"
)

// Runtime owns only assembled values. Deer owns every connection and task.
type Runtime struct {
	execution                   *executionbusiness.Execution
	market                      *marketbusiness.Market
	strategy                    *strategygrpc.Server
	strategyCatalog             *strategygrpc.CatalogServer
	strategyAudit               *strategygrpc.AuditServer
	strategyOrders              *strategygrpc.OrderQueryServer
	strategyTrades              *strategygrpc.TradeQueryServer
	strategyPortfolios          *strategygrpc.PortfolioQueryServer
	strategyRiskEvents          *strategygrpc.RiskEventQueryServer
	strategyOverview            *strategygrpc.OverviewQueryServer
	strategyMarkets             *strategygrpc.MarketQueryServer
	strategyRiskQuery           *strategygrpc.RiskQueryServer
	strategyRiskControl         *strategygrpc.RiskControlServer
	strategyCancellationQuery   *strategygrpc.CancellationQueryServer
	strategyCancellationControl *strategygrpc.CancellationControlServer
	strategyEmergencyQuery      *strategygrpc.EmergencyQueryServer
	strategyEmergencyControl    *strategygrpc.EmergencyControlServer
	strategyControl             *strategygrpc.ControlServer
	strategyCreation            *strategygrpc.CreationServer
	strategyPublication         *strategygrpc.PublicationServer
	trustedAdmin                bool
	modules                     []*Runtime
	workers                     []deer.Worker
}

// New retains historical combined/management assembly for regression fixtures.
// Core commands use the service-specific constructors instead.
func New(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Runtime, error) {
	db, incarnation, err := runtimeconfig.Data(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceOperator {
		return newOperator(ctx, db, owner, incarnation, cfg.Business)
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceMarket {
		return newMarket(ctx, db, incarnation, cfg.Business)
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceStrategy {
		return newStrategy(ctx, db, owner, incarnation, cfg.Business)
	}
	events, err := owner.JetStream()
	if err != nil {
		return nil, err
	}
	if cfg.Name+".grpc" == rpcsecurity.ServiceExecution {
		return newExecution(ctx, db, events, incarnation, cfg.Business)
	}
	return newPortfolio(ctx, db, owner, events, cfg.Business)
}

// Workers returns a private plan copy for the Deer lifecycle.
func (r *Runtime) Workers() []deer.Worker { return slices.Clone(r.workers) }

// RegisterGRPC registers the explicitly assembled business capabilities.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
	if r.trustedAdmin {
		registrar = adminactor.Registrar(registrar)
	}
	for _, module := range r.modules {
		if err := module.RegisterGRPC(registrar); err != nil {
			return err
		}
	}
	if r.execution != nil {
		if err := r.execution.RegisterGRPC(registrar); err != nil {
			return err
		}
	}
	if r.market != nil {
		if err := r.market.RegisterGRPC(registrar); err != nil {
			return err
		}
	}
	if r.strategy != nil {
		strategyv1.RegisterStrategyManagementServer(registrar, r.strategy)
	}
	if r.strategyControl != nil {
		strategyv1.RegisterStrategyControlServer(registrar, r.strategyControl)
	}
	if r.strategyCatalog != nil {
		strategyv1.RegisterStrategyCatalogServer(registrar, r.strategyCatalog)
	}
	if r.strategyAudit != nil {
		strategyv1.RegisterStrategyAuditServer(registrar, r.strategyAudit)
	}
	if r.strategyOrders != nil {
		strategyv1.RegisterStrategyOrderQueryServer(registrar, r.strategyOrders)
	}
	if r.strategyTrades != nil {
		strategyv1.RegisterStrategyTradeQueryServer(registrar, r.strategyTrades)
	}
	if r.strategyPortfolios != nil {
		strategyv1.RegisterStrategyPortfolioQueryServer(registrar, r.strategyPortfolios)
	}
	if r.strategyRiskEvents != nil {
		strategyv1.RegisterStrategyRiskEventQueryServer(registrar, r.strategyRiskEvents)
	}
	if r.strategyOverview != nil {
		strategyv1.RegisterStrategyOverviewQueryServer(registrar, r.strategyOverview)
	}
	if r.strategyMarkets != nil {
		strategyv1.RegisterStrategyMarketQueryServer(registrar, r.strategyMarkets)
	}
	if r.strategyRiskQuery != nil {
		strategyv1.RegisterStrategyRiskQueryServer(registrar, r.strategyRiskQuery)
	}
	if r.strategyRiskControl != nil {
		strategyv1.RegisterStrategyRiskControlServer(registrar, r.strategyRiskControl)
	}
	if r.strategyCancellationQuery != nil {
		strategyv1.RegisterStrategyOrderCancellationQueryServer(registrar, r.strategyCancellationQuery)
	}
	if r.strategyCancellationControl != nil {
		strategyv1.RegisterStrategyOrderCancellationControlServer(registrar, r.strategyCancellationControl)
	}
	if r.strategyEmergencyQuery != nil {
		strategyv1.RegisterStrategyEmergencyCancellationQueryServer(registrar, r.strategyEmergencyQuery)
	}
	if r.strategyEmergencyControl != nil {
		strategyv1.RegisterStrategyEmergencyCancellationControlServer(registrar, r.strategyEmergencyControl)
	}
	if r.strategyCreation != nil {
		strategyv1.RegisterStrategyCreationServer(registrar, r.strategyCreation)
	}
	if r.strategyPublication != nil {
		strategyv1.RegisterStrategyPublicationServer(registrar, r.strategyPublication)
	}
	return nil
}
