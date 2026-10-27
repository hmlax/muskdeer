package deer

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

func validateConfig(cfg *configv1.ServiceConfig, api bool) (*configv1.ServiceConfig, error) {
	if cfg == nil {
		return nil, errors.New("service configuration is required")
	}
	cfg = proto.CloneOf(cfg)
	for _, identity := range []string{cfg.Name, cfg.InstanceId, cfg.Version} {
		if strings.TrimSpace(identity) == "" || strings.TrimSpace(identity) != identity {
			return nil, errors.New("service name, instance ID and version must be nonempty without surrounding whitespace")
		}
	}
	for name, duration := range map[string]*durationpb.Duration{
		"startup timeout":  cfg.StartupTimeout,
		"shutdown timeout": cfg.ShutdownTimeout,
	} {
		if err := validateDuration(name, duration); err != nil {
			return nil, err
		}
	}
	if api != (cfg.Http != nil) {
		return nil, errors.New("HTTP listener is required only for NewDeerApi")
	}
	if cfg.Nacos.GetRegistration() != nil && cfg.Grpc == nil {
		return nil, errors.New("nacos publication requires a gRPC listener")
	}
	if cfg.GrpcSecurity != nil && cfg.Grpc == nil {
		return nil, errors.New("gRPC security requires a gRPC listener")
	}
	if cfg.GrpcClients != nil {
		if cfg.Nacos == nil {
			return nil, errors.New("gRPC clients require Nacos discovery")
		}
		if err := rpcclient.Validate(cfg.GrpcClients); err != nil {
			return nil, err
		}
		if cfg.GrpcClients.RequestTimeout.AsDuration() > cfg.ShutdownTimeout.AsDuration() {
			return nil, errors.New("gRPC client request timeout must fit within shutdown timeout")
		}
	}
	if cfg.Grpc != nil {
		if err := rpcsecurity.ValidateListener(cfg.Grpc.Address, cfg.Name+grpcServiceSuffix, cfg.GrpcSecurity); err != nil {
			return nil, err
		}
	}
	for name, listener := range map[string]*configv1.Listener{
		"grpc": cfg.Grpc,
		"http": cfg.Http,
	} {
		if listener == nil {
			continue
		}
		if strings.TrimSpace(listener.Address) == "" {
			return nil, fmt.Errorf("%s listener address is required", name)
		}
		if err := validateDuration(name+" request timeout", listener.RequestTimeout); err != nil {
			return nil, err
		}
		if listener.RequestTimeout.AsDuration() > cfg.ShutdownTimeout.AsDuration() {
			return nil, fmt.Errorf("%s request timeout must fit within shutdown timeout", name)
		}
	}
	return cfg, nil
}

func validateDuration(name string, duration *durationpb.Duration) error {
	if err := duration.CheckValid(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	value := duration.AsDuration()
	if value <= 0 || !proto.Equal(durationpb.New(value), duration) {
		return fmt.Errorf("%s must be a positive Go duration", name)
	}
	return nil
}
