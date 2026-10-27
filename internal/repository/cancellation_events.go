package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func appendCancellationEvent(ctx context.Context, tx *gorm.DB, row *modelv1.CancellationRequest, kind string) error {
	state, err := cancellationWireState(domain.CancellationState(row.State))
	if err != nil {
		return err
	}
	now, err := intentClock(ctx, tx, row.IntentId)
	if err != nil {
		return err
	}
	eventID := fmt.Sprintf("cancel:%d:%d", row.Id, row.Revision)
	return tx.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Create(&modelv1.OutboxEvent{
		EventId:   eventID,
		IntentId:  row.IntentId,
		EventType: kind,
		State:     domain.OutboxStatePending,
		Event: &commonv1.ExecutionEvent{
			EventId:          eventID,
			EventType:        kind,
			SchemaVersion:    domain.EventSchemaVersion,
			OccurredAtMs:     now,
			Producer:         domain.EventProducerExecution,
			AggregateId:      row.Id,
			AggregateVersion: row.Revision,
			RequestId:        row.RequestId,
			Cancellation: &commonv1.CancellationEvent{
				CancellationId:     row.Id,
				IntentId:           row.IntentId,
				AccountId:          row.AccountId,
				ClientOrderId:      row.ClientOrderId,
				StrategyInstanceId: row.StrategyInstanceId,
				State:              state,
				Revision:           row.Revision,
				ErrorCode:          row.ErrorCode,
			},
		},
		NextAttemptAt: now,
	}).Error
}

func cancellationWireState(state domain.CancellationState) (commonv1.CancellationState, error) {
	switch state {
	case domain.CancellationRequested:
		return commonv1.CancellationState_CANCELLATION_STATE_REQUESTED, nil
	case domain.CancellationSubmitting:
		return commonv1.CancellationState_CANCELLATION_STATE_SUBMITTING, nil
	case domain.CancellationPending:
		return commonv1.CancellationState_CANCELLATION_STATE_PENDING, nil
	case domain.CancellationConfirmed:
		return commonv1.CancellationState_CANCELLATION_STATE_CONFIRMED, nil
	case domain.CancellationUnknown:
		return commonv1.CancellationState_CANCELLATION_STATE_UNKNOWN, nil
	case domain.CancellationRejected:
		return commonv1.CancellationState_CANCELLATION_STATE_REJECTED, nil
	default:
		return commonv1.CancellationState_CANCELLATION_STATE_UNSPECIFIED, domain.ErrInvalidOrder
	}
}
