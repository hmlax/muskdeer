package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/quoting"
)

func (s *StrategyController) quote(ctx context.Context, spec domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot) error {
	environment, err := s.deps.Environment.Current(ctx, spec)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotencyConflict) {
			return errStrategyReobserve
		}
		if errors.Is(err, domain.ErrRiskDenied) || errors.Is(err, domain.ErrInvalidParameters) {
			return s.protectRecovery(ctx, spec, token, orders)
		}
		return err
	}
	if !environment.RiskAllowed && environment.RiskReason != domain.RiskReasonNotEvaluated {
		return s.protectRecovery(ctx, spec, token, orders)
	}
	if environment.Parameters.Template != domain.StrategyTemplateMarketMaker || environment.Parameters.Maker == nil {
		return domain.ErrInvalidParameters
	}
	p := *environment.Parameters.Maker
	reference, err := s.deps.Market.Reference(ctx, spec.ActiveSource, spec.InstrumentID, spec.Mode)
	if err != nil {
		return s.protectRecovery(ctx, spec, token, orders)
	}
	targets, err := quoting.Maker(quoting.Input{
		Instrument: quoting.Instrument{
			ID:              environment.Instrument.ID,
			MetadataVersion: environment.Instrument.MetadataVersion,
			Status:          environment.Instrument.Status,
			PriceTick:       environment.Instrument.PriceTick,
			QuantityStep:    environment.Instrument.QuantityStep,
			MinNotional:     environment.Instrument.MinNotional,
		},
		Source:         spec.ActiveSource,
		Symbol:         environment.Symbol,
		Mode:           spec.Mode,
		Market:         reference,
		Parameters:     p,
		Inventory:      environment.Inventory,
		AvailableBase:  environment.AvailableBase,
		AvailableQuote: environment.AvailableQuote,
		QuoteFeeRate:   environment.QuoteFeeRate,
		LocalBid:       environment.LocalBid,
		LocalAsk:       environment.LocalAsk,
		Now:            time.Now(),
	})
	if err != nil {
		return s.protectRecovery(ctx, spec, token, orders)
	}
	targets = fundedTargets(targets, environment)
	plan, err := quoting.Diff(quoting.DiffInput{
		StrategyID:         spec.ID,
		AccountID:          spec.AccountID,
		InstrumentID:       spec.InstrumentID,
		Targets:            targets,
		Orders:             orders,
		Now:                time.Now(),
		LastRefresh:        environment.LastRefresh,
		RepriceBPS:         p.RepriceBPS,
		MinRefreshInterval: p.MinRefreshInterval,
		MaxQuoteAge:        p.MaxQuoteAge,
	})
	if err != nil {
		return err
	}
	if len(plan.Cancel) != 0 {
		return s.cancelTarget(ctx, spec, token, orders, plan.Cancel[0])
	}
	if !environment.RiskAllowed || len(plan.Submit) == 0 {
		return nil
	}
	active, err := s.deps.Actions.Active(ctx, spec.ID)
	if err != nil || len(active) != 0 {
		return err
	}
	target := plan.Submit[0]
	return s.planAndDispatch(ctx, strategyAuthority(spec, token), domain.StrategyActionPlan{
		Kind:         domain.StrategyActionSubmit,
		AccountID:    spec.AccountID,
		InstrumentID: spec.InstrumentID,
		Side:         target.Side,
		Price:        target.Price,
		Quantity:     target.Quantity,
		TimeInForce:  target.TimeInForce,
	})
}

func (s *StrategyController) protectRecovery(ctx context.Context, spec domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot) error {
	recovering, err := s.transition(ctx, spec, token, domain.StrategyStateRecovering, false)
	if err != nil {
		return err
	}
	return s.protect(ctx, recovering, token, orders)
}

func (s *StrategyController) protect(ctx context.Context, spec domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot) error {
	plan, err := quoting.Diff(quoting.DiffInput{
		StrategyID:         spec.ID,
		AccountID:          spec.AccountID,
		InstrumentID:       spec.InstrumentID,
		Orders:             orders,
		Now:                time.Now(),
		MinRefreshInterval: time.Millisecond,
		MaxQuoteAge:        time.Hour,
		Protect:            true,
	})
	if err != nil || len(plan.Cancel) == 0 {
		return err
	}
	return s.cancelTarget(ctx, spec, token, orders, plan.Cancel[0])
}

func (s *StrategyController) cancelTarget(ctx context.Context, spec domain.StrategySpec, token domain.LeaseToken, orders []domain.IntentSnapshot, id int64) error {
	active, err := s.deps.Actions.Active(ctx, spec.ID)
	if err != nil {
		return err
	}
	for _, action := range active {
		if action.Plan.Kind == domain.StrategyActionCancel && action.Plan.TargetIntentID == id {
			return nil
		}
	}
	for _, order := range orders {
		if order.ID != id {
			continue
		}
		return s.planAndDispatch(ctx, strategyAuthority(spec, token), domain.StrategyActionPlan{
			Kind:                domain.StrategyActionCancel,
			AccountID:           spec.AccountID,
			InstrumentID:        spec.InstrumentID,
			TargetIntentID:      order.ID,
			TargetClientOrderID: order.ClientOrderID,
		})
	}
	return domain.ErrInvalidOrder
}

func fundedTargets(targets []quoting.Target, input domain.StrategyEnvironmentView) []quoting.Target {
	remaining := input.TotalNotional
	buyRoom := input.MaximumBuyQuantity
	result := make([]quoting.Target, 0, len(targets))
	for _, target := range targets {
		limit := decimal.Min(remaining, input.MaximumOrderNotional)
		lots, _ := limit.QuoRem(target.Price.Mul(input.Instrument.QuantityStep), 0)
		target.Quantity = decimal.Min(target.Quantity, lots.Mul(input.Instrument.QuantityStep))
		if target.Side == domain.SideBuy {
			buyLots, _ := buyRoom.QuoRem(input.Instrument.QuantityStep, 0)
			target.Quantity = decimal.Min(target.Quantity, buyLots.Mul(input.Instrument.QuantityStep))
		}
		notional := target.Price.Mul(target.Quantity)
		if !target.Quantity.IsPositive() || notional.LessThan(input.Instrument.MinNotional) {
			continue
		}
		remaining = remaining.Sub(notional)
		if target.Side == domain.SideBuy {
			buyRoom = buyRoom.Sub(target.Quantity)
		}
		result = append(result, target)
	}
	return result
}
