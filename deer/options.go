package deer

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/deer/jetstream"
	"github.com/hmlax/muskdeer/deer/logging"
	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/redis"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/tracing"
)

// Option injects an initialized dependency borrowed for the entire Deer lifetime.
// The caller closes injected dependencies after Deer cleanup, including on failure.
// Without an option, Deer creates and owns the dependency from configuration.
type Option interface {
	apply(*options) error
}

type options struct {
	tracing    *tracing.Runtime
	logger     *logging.Logger
	database   *postgres.Client
	cache      *redis.Client
	events     *jetstream.Client
	discovery  *nacos.Client
	rpcClients *rpcclient.Factory
}

type loggerOption struct{ logger *logging.Logger }
type databaseOption struct{ database *postgres.Client }
type redisOption struct{ cache *redis.Client }
type jetStreamOption struct{ events *jetstream.Client }
type discoveryOption struct{ discovery *nacos.Client }
type grpcClientsOption struct{ clients *rpcclient.Factory }

// WithLogger borrows a JSON logger instead of creating the configured logger.
func WithLogger(logger *logging.Logger) Option { return loggerOption{logger: logger} }

// WithDB borrows a PostgreSQL client and exposes its GORM handle through DB.
func WithDB(database *postgres.Client) Option { return databaseOption{database: database} }

// WithRedis borrows a cache client instead of opening another Redis pool.
func WithRedis(cache *redis.Client) Option { return redisOption{cache: cache} }

// WithJetStream borrows an initialized publisher instead of opening a connection.
func WithJetStream(events *jetstream.Client) Option { return jetStreamOption{events: events} }

// WithDiscovery borrows a Nacos client; Deer still owns its service publication.
func WithDiscovery(discovery *nacos.Client) Option { return discoveryOption{discovery: discovery} }

// WithGRPCClients borrows a factory instead of constructing outbound RPC clients.
func WithGRPCClients(clients *rpcclient.Factory) Option { return grpcClientsOption{clients: clients} }

func (o loggerOption) apply(settings *options) error {
	if o.logger == nil {
		return errors.New("injected logger is required")
	}
	settings.logger = o.logger
	return nil
}

func (o databaseOption) apply(settings *options) error {
	if o.database == nil {
		return errors.New("injected postgres client is required")
	}
	settings.database = o.database
	return nil
}

func (o redisOption) apply(settings *options) error {
	if o.cache == nil {
		return errors.New("injected redis client is required")
	}
	settings.cache = o.cache
	return nil
}

func (o jetStreamOption) apply(settings *options) error {
	if o.events == nil {
		return errors.New("injected jetstream client is required")
	}
	settings.events = o.events
	return nil
}

func (o discoveryOption) apply(settings *options) error {
	if o.discovery == nil {
		return errors.New("injected nacos client is required")
	}
	settings.discovery = o.discovery
	return nil
}

func (o grpcClientsOption) apply(settings *options) error {
	if o.clients == nil {
		return errors.New("injected gRPC client factory is required")
	}
	settings.rpcClients = o.clients
	return nil
}

func resolveOptions(values []Option) (options, error) {
	var settings options
	for _, option := range values {
		if option == nil {
			return options{}, errors.New("service option is required")
		}
		if err := option.apply(&settings); err != nil {
			return options{}, err
		}
	}
	return settings, nil
}

// borrowedResource checks an injected dependency without taking its ownership.
func borrowedResource(name string, check func(context.Context) error) Initializer {
	return Initializer{
		Name: name,
		Open: func(ctx context.Context) (Cleanup, error) {
			if err := check(ctx); err != nil {
				return nil, err
			}
			return func(context.Context) error { return nil }, nil
		},
	}
}
