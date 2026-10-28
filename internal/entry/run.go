// Package entry runs one configured service role in one independent process.
package entry

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/cloudwego/hertz/pkg/app/server"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/business"
	"github.com/hmlax/muskdeer/internal/delivery/adminhttp"
	"github.com/hmlax/muskdeer/internal/processlog"
)

// Role selects a separately owned service process, not a module in one process.
type Role string

const (
	// RoleAPI owns Hertz administration delivery; business routes arrive in P07.
	RoleAPI Role = "api"
	// RoleMarket owns market-data work introduced in P05.
	RoleMarket Role = "market"
	// RoleStrategy owns strategy work introduced in P06/P07.
	RoleStrategy Role = "strategy"
	// RoleExecution owns execution work introduced in P03/P04.
	RoleExecution Role = "execution"
	// RolePortfolio owns portfolio work introduced in P04.
	RolePortfolio Role = "portfolio"
	// RoleCron owns scheduled work introduced in P09.
	RoleCron Role = "cron"
)

// ParseRole accepts only the six explicit service identities.
func ParseRole(value string) (Role, error) {
	role := Role(value)
	switch role {
	case RoleAPI, RoleMarket, RoleStrategy, RoleExecution, RolePortfolio, RoleCron:
		return role, nil
	default:
		return "", errors.New("service role must be api, market, strategy, execution, portfolio or cron")
	}
}

// Run validates a role/config match, creates one Deer (DeerApi for api), and
// joins Run before returning. SDK logging is installed before dependencies or
// transports use SDK globals. Call only once in a fresh, sole service process.
func Run(ctx context.Context, role Role, cfg *configv1.ServiceConfig, output io.Writer) error {
	if ctx == nil || cfg == nil {
		return errors.New("service entry requires context and configuration")
	}
	cfg = proto.CloneOf(cfg)
	if err := business.ValidateConfig(cfg); err != nil {
		return err
	}
	if err := adminhttp.ValidateConfig(cfg); err != nil {
		return err
	}
	if _, err := ParseRole(string(role)); err != nil {
		return err
	}
	if cfg.Name != string(role) || cfg.Monitoring == nil {
		return errors.New("service entry requires a matching name and local monitoring listener")
	}
	if role == RoleAPI && cfg.Http == nil || role != RoleAPI && cfg.Http != nil {
		return errors.New("only api requires a business HTTP listener")
	}
	if role != RoleAPI && role != RoleCron && cfg.Grpc == nil {
		return errors.New("business service roles require a gRPC listener")
	}
	var logger *logging.Logger
	spec := deer.ServiceSpec{
		LogOutput: output,
		SetupProcessLogging: func(ctx context.Context, owned *logging.Logger) (deer.Cleanup, error) {
			logger = owned
			return processlog.Install(ctx, owned)
		},
		Resources: []deer.Initializer{
			{
				Name: "service.lifecycle",
				Open: func(context.Context) (deer.Cleanup, error) {
					return func(ctx context.Context) error {
						return logger.LogAttrs(ctx, slog.LevelInfo, "service.transports_stopped")
					}, nil
				},
			},
		},
	}
	if cfg.GetBusiness().GetEnabled() {
		var runtime *business.Runtime
		spec.Workers = func(ctx context.Context, owner *deer.Deer) ([]deer.Worker, error) {
			var err error
			runtime, err = business.New(ctx, cfg, owner)
			if err != nil {
				return nil, err
			}
			return runtime.Workers(), nil
		}
		spec.RegisterGRPC = func(_ context.Context, _ *deer.Deer, registrar grpc.ServiceRegistrar) error {
			return runtime.RegisterGRPC(registrar)
		}
	}
	var (
		owner       *deer.Deer
		cleanup     deer.Cleanup
		httpAddress string
		err         error
	)
	if role == RoleAPI {
		var api *deer.DeerApi
		api, cleanup, err = deer.NewDeerApi(ctx, cfg, deer.APISpec{
			Service: spec,
			Routes: func(ctx context.Context, owner *deer.Deer, hertz *server.Hertz) error {
				return adminhttp.Assemble(ctx, cfg, owner, hertz)
			},
		})
		if err == nil {
			owner = api.Deer
			httpAddress = api.HTTPAddress()
		}
	} else {
		owner, cleanup, err = deer.NewDeer(ctx, cfg, spec)
	}
	if err != nil {
		return err
	}
	return runOwned(ctx, owner, cleanup, httpAddress)
}

func runOwned(ctx context.Context, owner *deer.Deer, cleanup deer.Cleanup, httpAddress string) (result error) {
	defer func() { result = errors.Join(result, cleanup(ctx)) }()
	if err := owner.Logger().LogAttrs(ctx, slog.LevelInfo, "service.initialized",
		slog.String("grpc_address", owner.GRPCAddress()),
		slog.String("http_address", httpAddress),
		slog.String("monitoring_address", owner.MonitoringAddress())); err != nil {
		return err
	}
	return owner.Run(ctx)
}
