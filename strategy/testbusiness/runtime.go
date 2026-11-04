// Package testbusiness assembles strategy management only for historical regression processes.
package testbusiness

import (
	"context"
	"slices"

	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	strategyv1 "github.com/hmlax/muskdeer/deer/pb/strategy/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
	"github.com/hmlax/muskdeer/strategy/delivery/strategygrpc"
)

// Runtime owns only assembled values. Deer owns every connection and task.
type Runtime struct {
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
	workers                     []deer.Worker
}

// New borrows the process resources and assembles only strategy work.
func New(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*Runtime, error) {
	db, incarnation, err := runtimeconfig.Data(ctx, cfg, owner)
	if err != nil {
		return nil, err
	}
	return newStrategy(ctx, db, owner, incarnation, cfg.Business)
}

// Workers returns a private plan copy for the Deer lifecycle.
func (r *Runtime) Workers() []deer.Worker { return slices.Clone(r.workers) }

// RegisterGRPC registers the explicitly assembled business capabilities.
func (r *Runtime) RegisterGRPC(registrar grpc.ServiceRegistrar) error {
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
