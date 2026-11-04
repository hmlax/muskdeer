package deer

import (
	"context"

	"github.com/hmlax/muskdeer/deer/jetstream"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	"github.com/hmlax/muskdeer/deer/redis"
	"github.com/hmlax/muskdeer/deer/rpcclient"
)

// initialize acquires only missing dependencies and checks borrowed clients.
// The resulting cleanup releases the resources created here, in reverse order.
func (o *options) initialize(ctx context.Context, cfg *configv1.ServiceConfig, spec ServiceSpec) (Cleanup, error) {
	plan := make([]Initializer, 0, len(spec.Resources)+7)
	if o.logger == nil {
		plan = append(plan, Initializer{
			Name: "deer.logging",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.logger, err = logging.New(ctx, cfg.Logging, logging.Identity{
					Service:  cfg.Name,
					Instance: cfg.InstanceId,
				}, spec.LogOutput)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.logger.Close() }, nil
			},
		})
	}
	if spec.SetupProcessLogging != nil {
		plan = append(plan, Initializer{
			Name: "deer.process-logging",
			Open: func(ctx context.Context) (Cleanup, error) {
				return spec.SetupProcessLogging(ctx, o.logger)
			},
		})
	}
	if o.database != nil {
		plan = append(plan, borrowedResource("deer.postgres", o.database.Ping))
	} else if cfg.Postgres != nil {
		plan = append(plan, Initializer{
			Name: "deer.postgres",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.database, err = postgres.New(ctx, cfg.Postgres, cfg.Name+"/"+cfg.InstanceId)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.database.Close() }, nil
			},
		})
	}
	if o.cache != nil {
		plan = append(plan, borrowedResource("deer.redis", o.cache.Ping))
	} else if cfg.Redis != nil {
		plan = append(plan, Initializer{
			Name: "deer.redis",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.cache, err = redis.New(ctx, cfg.Redis, cfg.Name+"/"+cfg.InstanceId)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.cache.Close() }, nil
			},
		})
	}
	if o.events != nil {
		plan = append(plan, borrowedResource("deer.jetstream", o.events.Ping))
	} else if cfg.Jetstream != nil {
		plan = append(plan, Initializer{
			Name: "deer.jetstream",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.events, err = jetstream.New(ctx, cfg.Jetstream, cfg.Name+"/"+cfg.InstanceId)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.events.Close() }, nil
			},
		})
	}
	if o.discovery != nil {
		plan = append(plan, borrowedResource("deer.nacos", o.discovery.Ping))
	} else if cfg.Nacos != nil {
		plan = append(plan, Initializer{
			Name: "deer.nacos",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.discovery, err = nacos.New(ctx, cfg.Nacos)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.discovery.Close() }, nil
			},
		})
	}
	if o.rpcClients != nil {
		plan = append(plan, borrowedResource("deer.grpc-clients", func(context.Context) error { return o.rpcClients.Health() }))
	} else if cfg.GrpcClients != nil {
		plan = append(plan, Initializer{
			Name: "deer.grpc-clients",
			Open: func(ctx context.Context) (Cleanup, error) {
				var err error
				o.rpcClients, err = rpcclient.New(ctx, cfg.GrpcClients, cfg.Name+grpcServiceSuffix, o.discovery)
				if err != nil {
					return nil, err
				}
				return func(context.Context) error { return o.rpcClients.Close() }, nil
			},
		})
	}
	plan = append(plan, spec.Resources...)
	return initializeResources(ctx, plan, cfg.ShutdownTimeout.AsDuration())
}
