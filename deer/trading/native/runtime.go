package native

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// ValidateConfig explicitly selects the SDK development route and its resources.
func ValidateConfig(cfg *configv1.ServiceConfig) error {
	b := cfg.GetBusiness()
	h := b.GetHimalaya()
	if h == nil || b.Market != nil || b.Strategy != nil || b.Simulator != nil || b.Risk != nil || b.ExecutionLeaseTtl != nil || b.PortfolioConsumer != "" ||
		len(h.Profiles) == 0 || len(h.Profiles) > 8 || h.Depth == 0 || h.Depth > 20 || h.LeaseTtl.CheckValid() != nil ||
		h.LeaseTtl.AsDuration() < 3*b.OperationTimeout.AsDuration() || h.LeaseTtl.AsDuration() > domain.MaxLeaseTTL {
		return ErrInvalid
	}
	fraction, err := Value(h.QuantityFraction, false, false)
	if err != nil || fraction.GreaterThan(decimal.NewFromInt(1)) {
		return ErrInvalid
	}
	if err := validateNativeRole(cfg, h); err != nil {
		return err
	}
	funds, symbols := make(map[string]bool), make(map[string]bool)
	for _, profile := range h.Profiles {
		if !validProfile(profile) || funds[ScopeKey(profile.UserId, profile.FundId)] || symbols[profile.Symbol] {
			return ErrInvalid
		}
		funds[ScopeKey(profile.UserId, profile.FundId)], symbols[profile.Symbol] = true, true
	}
	return ValidateSyntheticConfig(h)
}

func validateNativeRole(cfg *configv1.ServiceConfig, h *configv1.HimalayaRuntime) error {
	switch cfg.Name {
	case "market":
		return nil
	case "strategy", "portfolio":
		if cfg.GrpcClients == nil {
			return ErrInvalid
		}
	case "execution":
		host, port, err := net.SplitHostPort(h.Target)
		number, e2 := strconv.ParseUint(port, 10, 16)
		local := host == "127.0.0.1" || host == "localhost" || host == "orderbook-order-service-1" || host == "order-service"
		if err != nil || e2 != nil || number == 0 || !local || !environmentName(h.TokenEnv) || cfg.GrpcClients == nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validProfile(p *configv1.HimalayaProfile) bool {
	if p == nil || p.UserId <= 0 || p.FundId <= 0 || !Symbol(p.Symbol) || p.Leverage == 0 || p.Leverage > 10 || p.MarketDataSource != Source {
		return false
	}
	maximum, e1 := domain.ParseLedgerDecimal(p.MaximumNotional, false, false)
	order, e2 := domain.ParseLedgerDecimal(p.MaximumOrderNotional, false, false)
	_, e3 := Value(p.PriceTick, false, false)
	_, e4 := Value(p.QuantityStep, false, false)
	return e1 == nil && e2 == nil && e3 == nil && e4 == nil && !order.GreaterThan(maximum)
}

func environmentName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' {
			continue
		}
		return false
	}
	return true
}

// Authority snapshots a retained token without making its expiry authoritative.
func Authority(token domain.LeaseToken) *commonv1.HimalayaAuthority {
	return &commonv1.HimalayaAuthority{
		AccountId:   token.Key.AccountID,
		OwnerId:     token.OwnerID,
		Epoch:       token.Epoch,
		ExpiresAtMs: token.ExpiresAt.UnixMilli(),
	}
}

// Wait bounds idle/recovery without retaining a goroutine or abandoned timer.
func Wait(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
