package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type simulatorScenarios struct{ db *gorm.DB }

// NewSimulatorScenarios borrows storage; it neither creates nor confirms work.
func NewSimulatorScenarios(db *gorm.DB) (domain.SimulatorScenarios, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &simulatorScenarios{db: db}, nil
}

func (s *simulatorScenarios) DueCancellation(ctx context.Context, accountID string, delay time.Duration) (string, error) {
	if err := coreContext(ctx); err != nil {
		return "", err
	}
	if !coreIdentifier(accountID, 64) || delay < time.Millisecond || delay > 5*time.Second || delay%time.Millisecond != 0 {
		return "", domain.ErrInvalidOrder
	}
	// Commands are immutable. Partial fills update the order, but cannot move
	// the original cancellation request's database-clock eligibility time.
	commands := s.db.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Select("id").
		Where("account_id = ? AND action = ? AND delete_at IS NULL", accountID, domain.GatewayActionCancel).
		Where("create_at <= floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint - ?", delay.Milliseconds())
	var row modelv1.SimulatorOrder
	err := s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("ClientOrderId").
		Where("account_id = ? AND cancel_state = ? AND state IN ? AND last_command_id IN (?) AND delete_at IS NULL",
			accountID, domain.CancelStatePending, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}, commands).
		Order("id ASC").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", coreFailure(ctx, err)
	}
	return row.ClientOrderId, nil
}
