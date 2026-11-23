package native

import (
	"context"
	"log/slog"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	values "github.com/hmlax/muskdeer/deer/trading/native"
)

const workerPollControls = "portfolio.native.controls"

// A single process-owned reader refreshes observation admission for every fund.
// Failure keeps the last known read-only admission. This never authorizes an
// order; execution still checks current policy and source at its final send.
func (r *Runtime) watchPolling(ctx context.Context) error {
	failed := false
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, time.Second)
		err := r.refreshPolling(op)
		cancel()
		if err != nil && !failed && ctx.Err() == nil {
			if err := r.logger.LogAttrs(ctx, slog.LevelWarn, values.StateRecovering, slog.String("component", workerPollControls)); err != nil {
				return err
			}
		}
		failed = err != nil
		if err := values.Wait(ctx, 500*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runtime) refreshPolling(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	keys := sortedDependencyKeys(r.children)
	var rows []*model.TradingAccountPolicy
	if err := r.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).
		Select("account_id, enabled").Where("account_id IN ? AND delete_at IS NULL", keys).
		Find(&rows).Error; err != nil {
		return err
	}
	enabled := make(map[string]bool, len(rows))
	for _, row := range rows {
		enabled[row.AccountId] = row.Enabled
	}
	for key, child := range r.children {
		// Grid recovery can need observations while its account is disabled.
		// Keep all configured funds/inventory and manual InspectAccount intact.
		child.observeEnabled.Store(enabled[key] || child.observeRecovery || child.cfg.LongGrid != nil)
	}
	return nil
}
