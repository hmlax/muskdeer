// Package business assembles L1 application work from Deer-owned dependencies.
package business

import (
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

// ErrInvalidConfig fails explicit business startup without side effects.
var ErrInvalidConfig = errors.New("business mode requires supported role, explicit simulator and complete owned dependencies")

// ValidateConfig checks business-specific requirements without reading secrets,
// opening connections, migrating or provisioning broker resources.
func ValidateConfig(cfg *configv1.ServiceConfig) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	if !cfg.GetBusiness().GetEnabled() {
		return nil
	}
	if cfg.Postgres == nil || cfg.Jetstream == nil || cfg.Grpc == nil || cfg.GrpcSecurity.GetMtls() == nil || cfg.Nacos.GetRegistration() == nil ||
		cfg.Business.ExecutionVenue != domain.ExecutionVenueSimulator ||
		!boundedDuration(cfg.Business.PollInterval, 10*time.Millisecond, 5*time.Second) ||
		!boundedDuration(cfg.Business.OperationTimeout, 100*time.Millisecond, 5*time.Second) ||
		cfg.Business.OperationTimeout.AsDuration() > cfg.ShutdownTimeout.AsDuration() {
		return ErrInvalidConfig
	}
	switch cfg.Name + ".grpc" {
	case rpcsecurity.ServiceExecution:
		if cfg.Business.PortfolioConsumer != "" {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServicePortfolio:
		if cfg.GrpcClients == nil || !consumerName(cfg.Business.PortfolioConsumer) {
			return ErrInvalidConfig
		}
	default:
		return ErrInvalidConfig
	}
	return nil
}

func boundedDuration(value *durationpb.Duration, minimum, maximum time.Duration) bool {
	if value.CheckValid() != nil {
		return false
	}
	duration := value.AsDuration()
	return duration >= minimum && duration <= maximum && duration%time.Millisecond == 0
}

func consumerName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for index := range len(name) {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}
