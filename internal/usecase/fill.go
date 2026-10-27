package usecase

import (
	"context"

	"github.com/hmlax/muskdeer/internal/domain"
)

// FillConsumer commits the portfolio inbox before querying the authoritative
// order. ACK is allowed only after both portfolio and OMS effects succeed.
type FillConsumer struct {
	portfolio domain.PortfolioRepository
	intents   domain.IntentService
}

// NewFillConsumer borrows repositories/services and starts no background work.
func NewFillConsumer(portfolio domain.PortfolioRepository, intents domain.IntentService) (*FillConsumer, error) {
	if portfolio == nil || intents == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &FillConsumer{
		portfolio: portfolio,
		intents:   intents,
	}, nil
}

// Apply never submits an order. A failure after either commit is recoverable by
// replaying the same notice; inbox and core revision checks prevent double effects.
func (c *FillConsumer) Apply(ctx context.Context, notice domain.FillNotice) error {
	if _, err := c.portfolio.Apply(ctx, notice); err != nil {
		return err
	}
	confirmed, err := c.intents.Recover(ctx, notice.Fact.AccountID, notice.Fact.IntentID)
	if err != nil {
		return err
	}
	if confirmed.State == domain.OrderStateUnknown || confirmed.CoreOrderID == 0 {
		return domain.ErrOutcomeUnknown
	}
	if confirmed.CoreOrderID != notice.Fact.CoreOrderID || confirmed.Input.AccountID != notice.Fact.AccountID || confirmed.Input.InstrumentID != notice.Fact.InstrumentID || confirmed.Input.Authority.StrategyID != notice.Fact.StrategyID || confirmed.FilledQuantity.LessThan(notice.Fact.Quantity) {
		return domain.ErrInvalidOrder
	}
	return nil
}
