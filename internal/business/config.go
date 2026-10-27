// Package business assembles L1 application work from Deer-owned dependencies.
package business

import (
	"errors"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketworker"
)

// MessageInvalidConfig is a fixed startup failure without config contents.
const MessageInvalidConfig = "business mode requires supported role, explicit simulator and complete owned dependencies"

// ErrInvalidConfig fails explicit business startup without side effects.
var ErrInvalidConfig = errors.New(MessageInvalidConfig)

// ValidateConfig checks business-specific requirements without reading secrets,
// opening connections, migrating or provisioning broker resources.
func ValidateConfig(cfg *configv1.ServiceConfig) error {
	if cfg == nil {
		return ErrInvalidConfig
	}
	if !cfg.GetBusiness().GetEnabled() {
		return nil
	}
	if cfg.Postgres == nil || cfg.Grpc == nil || cfg.GrpcSecurity.GetMtls() == nil || cfg.Nacos.GetRegistration() == nil ||
		cfg.Business.ExecutionVenue != domain.ExecutionVenueSimulator ||
		!boundedDuration(cfg.Business.PollInterval, 10*time.Millisecond, 5*time.Second) ||
		!boundedDuration(cfg.Business.OperationTimeout, 100*time.Millisecond, 5*time.Second) ||
		cfg.Business.OperationTimeout.AsDuration() > cfg.ShutdownTimeout.AsDuration() {
		return ErrInvalidConfig
	}
	switch cfg.Name + ".grpc" {
	case rpcsecurity.ServiceExecution:
		if cfg.Jetstream == nil || cfg.Business.PortfolioConsumer != "" || cfg.Business.Market != nil {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServicePortfolio:
		if cfg.Jetstream == nil || cfg.GrpcClients == nil || !consumerName(cfg.Business.PortfolioConsumer) || cfg.Business.Market != nil {
			return ErrInvalidConfig
		}
	case rpcsecurity.ServiceMarket:
		if cfg.Business.PortfolioConsumer != "" || validateMarketConfig(cfg.Business) != nil {
			return ErrInvalidConfig
		}
	default:
		return ErrInvalidConfig
	}
	return nil
}

func validateMarketConfig(cfg *configv1.BusinessRuntime) error {
	if cfg.Market == nil || len(cfg.Market.Feeds) == 0 || len(cfg.Market.Feeds) > 16 ||
		!boundedDuration(cfg.Market.LeaseTtl, domain.MinLeaseTTL, domain.MaxLeaseTTL) || cfg.Market.LeaseTtl.AsDuration() <= 2*cfg.OperationTimeout.AsDuration() {
		return ErrInvalidConfig
	}
	seen := make(map[int64]struct{}, len(cfg.Market.Feeds))
	for _, feed := range cfg.Market.Feeds {
		if feed == nil || feed.FeedId <= 0 {
			return ErrInvalidConfig
		}
		if _, duplicate := seen[feed.FeedId]; duplicate {
			return ErrInvalidConfig
		}
		seen[feed.FeedId] = struct{}{}
		if hasReplay(feed) {
			if _, err := replaySettings(feed, cfg.OperationTimeout.AsDuration()); err != nil {
				return err
			}
		}
	}
	return nil
}

func hasReplay(cfg *configv1.MarketFeedRuntime) bool {
	return cfg.ReplayBid != "" || cfg.ReplayAsk != "" || cfg.ReplayQuantity != "" || cfg.ReplayInterval != nil
}

func replaySettings(cfg *configv1.MarketFeedRuntime, timeout time.Duration) (marketworker.ReplayConfig, error) {
	if !boundedDuration(cfg.ReplayInterval, 10*time.Millisecond, time.Second) {
		return marketworker.ReplayConfig{}, ErrInvalidConfig
	}
	rule := domain.DecimalRule{
		IntegerDigits: 20,
		Scale:         18,
	}
	bid, err := domain.ParseDecimal(cfg.ReplayBid, rule)
	if err != nil {
		return marketworker.ReplayConfig{}, ErrInvalidConfig
	}
	ask, err := domain.ParseDecimal(cfg.ReplayAsk, rule)
	if err != nil || !bid.LessThan(ask) {
		return marketworker.ReplayConfig{}, ErrInvalidConfig
	}
	quantity, err := domain.ParseDecimal(cfg.ReplayQuantity, rule)
	if err != nil {
		return marketworker.ReplayConfig{}, ErrInvalidConfig
	}
	return marketworker.ReplayConfig{
		Bid:             bid,
		Ask:             ask,
		Quantity:        quantity,
		Interval:        cfg.ReplayInterval.AsDuration(),
		CallbackTimeout: timeout,
	}, nil
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
