package venue

import (
	"context"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
)

type syntheticHistory struct {
	count      int64
	last       int64
	lastSource int64
	total      decimal.Decimal
	replay     *model.SyntheticTapeIntent
}

// The execution lease is already locked. Read every consumed slot, including
// unknown outcomes, in bounded pages without limiting a run's total length.
func readSyntheticHistory(ctx context.Context, tx *gorm.DB, cfg *common.VenuePolicy, plan *common.SyntheticTapePlan, digest string) (*syntheticHistory, error) {
	const pageSize = 256
	history := &syntheticHistory{total: decimal.Zero}
	for {
		var rows []*model.SyntheticTapeIntent
		query := tx.Session(&gorm.Session{
			NewDB:   true,
			Context: ctx,
		}).Model(&model.SyntheticTapeIntent{}).
			// Historical validation uses the pricing/slot/source-trade fields,
			// not the former live book or strategy lease. Exclude those large
			// snapshots at the storage boundary; stored audit stays complete.
			// A replay returns Result only, never this partial egress authority.
			Select("id, create_at, update_at, delete_at, run_id, symbol, sequence, policy_digest, (plan - 'book' - 'strategy') AS plan, notional, result").
			Where("run_id = ? AND symbol = ?", plan.RunId, plan.Symbol)
		if history.count > 0 {
			query = query.Where("sequence >= ?", history.count)
		}
		if err := query.Order("sequence").Limit(pageSize).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			if err := history.consume(cfg, row, digest); err != nil {
				return nil, err
			}
			if row.Sequence == int64(plan.Sequence) {
				history.replay = row
			}
		}
		if len(rows) < pageSize {
			return history, nil
		}
	}
}

func (h *syntheticHistory) consume(cfg *common.VenuePolicy, row *model.SyntheticTapeIntent, digest string) error {
	amount, err := syntheticReservation(row, digest, h.count)
	if err != nil {
		return err
	}
	if cfg.SyntheticTape.Mode == config.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		sourceID, err := validateSyntheticTradeAudit(cfg, row.Plan)
		if err != nil || sourceID <= h.lastSource {
			return ErrInvalidSubmit
		}
		h.lastSource = sourceID
	} else if row.Plan.GetSourceTrade() != nil {
		return ErrInvalidSubmit
	}
	h.total = h.total.Add(amount)
	h.last = max(h.last, row.CreateAt)
	h.count++
	return nil
}
