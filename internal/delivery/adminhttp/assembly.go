package adminhttp

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app/server"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/migrate"
	"github.com/hmlax/muskdeer/internal/repository"
)

// Assemble borrows the API's sole Deer database and checks the migration ledger.
// It runs no migration, creates no user and owns no background work.
func Assemble(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer, hertz *server.Hertz) error {
	if err := ValidateConfig(cfg); err != nil {
		return err
	}
	if !cfg.GetAdminHttp().GetEnabled() {
		return nil
	}
	if owner == nil || hertz == nil || hertz.GetOptions().Listener == nil {
		return ErrInvalidConfig
	}
	db, err := owner.DB()
	if err != nil {
		return err
	}
	if err := migrate.CheckCurrent(ctx, db); err != nil {
		return err
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		return err
	}
	origin, secure, err := resolvedOrigin(cfg.AdminHttp, hertz.GetOptions().Listener.Addr())
	if err != nil {
		return err
	}
	routes, err := New(auth, origin, secure)
	if err != nil {
		return err
	}
	if cfg.AdminHttp.StrategyQueriesEnabled {
		factory, err := owner.GRPCClients()
		if err != nil {
			return err
		}
		connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceStrategy})
		if err != nil {
			return err
		}
		routes.strategies = strategyv1.NewStrategyManagementClient(connection)
		routes.catalog = strategyv1.NewStrategyCatalogClient(connection)
		routes.audits = strategyv1.NewStrategyAuditClient(connection)
		routes.riskQueries = strategyv1.NewStrategyRiskQueryClient(connection)
		if cfg.AdminHttp.StrategyControlsEnabled {
			routes.controls = strategyv1.NewStrategyControlClient(connection)
			routes.creation = strategyv1.NewStrategyCreationClient(connection)
			routes.publication = strategyv1.NewStrategyPublicationClient(connection)
			routes.riskControls = strategyv1.NewStrategyRiskControlClient(connection)
		}
	}
	routes.Register(hertz)
	return nil
}
