package venue

import (
	"context"
	"errors"
	"strconv"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func gridDB(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.Session(&gorm.Session{
		NewDB:   true,
		Context: ctx,
	})
}

func (r *Runtime) gridControl(ctx context.Context) (*strategy.StrategyInstance, error) {
	var control strategy.StrategyInstance
	err := gridDB(ctx, r.db).Model(&strategy.StrategyInstance{}).Where("id = ? AND revision = ? AND delete_at IS NULL", r.cfg.InstanceID, r.cfg.Revision).Take(&control).Error
	if err != nil {
		return nil, err
	}
	if control.AccountId != AccountID(r.cfg.Venue.Profiles[0].UserId, r.cfg.Venue.Profiles[0].FundId) {
		return nil, longgrid.ErrInvalid
	}
	return &control, nil
}

func (r *Runtime) gridCursor(ctx context.Context, control *strategy.StrategyInstance, snapshot *common.VenueSnapshot) (*model.LongGridRuntime, error) {
	var state model.LongGridRuntime
	err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Where("instance_id = ?", r.cfg.InstanceID).Take(&state).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return &state, err
	}
	quantity, _, _, err := longgrid.Position(snapshot)
	if err != nil || quantity.IsPositive() {
		return nil, longgrid.ErrInvalid
	}
	if !control.DesiredRunning {
		return nil, nil
	}
	state = model.LongGridRuntime{
		InstanceId:   r.cfg.InstanceID,
		Revision:     r.cfg.Revision,
		State:        longgrid.Running,
		NextBuyPrice: "0",
		Lots:         &common.LongGridLots{},
	}
	if err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "instance_id"}},
		DoNothing: true,
	}).Create(&state).Error; err != nil {
		return nil, err
	}
	if err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Where("instance_id = ?", r.cfg.InstanceID).Take(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func lockGridCursor(ctx context.Context, tx *gorm.DB, id int64) (*model.LongGridRuntime, error) {
	var state model.LongGridRuntime
	err := gridDB(ctx, tx).Model(&model.LongGridRuntime{}).Where("instance_id = ? AND delete_at IS NULL", id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&state).Error
	return &state, err
}

func saveGridCursor(ctx context.Context, tx *gorm.DB, state *model.LongGridRuntime) error {
	return gridDB(ctx, tx).Model(&model.LongGridRuntime{}).Where("id = ?", state.Id).
		Select("Revision", "State", "Started", "NextBuyPrice", "Lots", "PendingActionId", "StopActionId", "NextSequence", "HaltedControlVersion").Updates(state).Error
}

func (r *Runtime) claimGridAction(ctx context.Context, observed *model.LongGridRuntime, snapshot *common.VenueSnapshot, book *common.VenueBook, decision longgrid.Decision) (*model.LongGridAction, error) {
	var action *model.LongGridAction
	err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Transaction(func(tx *gorm.DB) error {
		profile := r.cfg.Venue.Profiles[0]
		if decision.Side == domain.SideBuy {
			if err := lockCurrentInstance(ctx, tx, r.cfg.InstanceID, r.cfg.Revision, AccountID(profile.UserId, profile.FundId), profile.Symbol, profile.MarketDataSource, domain.StrategyTemplateGrid); err != nil {
				return err
			}
		}
		state, err := lockGridCursor(ctx, tx, r.cfg.InstanceID)
		if err != nil {
			return err
		}
		if state.Revision != r.cfg.Revision || state.NextSequence != observed.NextSequence {
			return domain.ErrLeaseHeld
		}
		stop := decision.Kind == longgrid.StopLoss
		if stop {
			if state.State != longgrid.Stopping || state.StopActionId != nil {
				return domain.ErrLeaseHeld
			}
		} else if state.State != longgrid.Running || state.PendingActionId != nil {
			return domain.ErrLeaseHeld
		}
		if snapshot.ValidUntilMs <= nowMillis() {
			return native.ErrInvalid
		}
		if decision.Side == domain.SideBuy {
			if !validGridBook(book, profile) {
				return native.ErrInvalid
			}
			if err := r.gridOpeningRisk(ctx, tx, snapshot, decision); err != nil {
				return err
			}
		}
		action = &model.LongGridAction{
			InstanceId:       r.cfg.InstanceID,
			Revision:         r.cfg.Revision,
			Sequence:         state.NextSequence,
			ClientId:         native.InstanceClientPrefix(profile) + "grid_" + strconv.FormatInt(state.NextSequence, 10),
			Kind:             decision.Kind,
			Side:             string(decision.Side),
			Price:            decision.Price.String(),
			Quantity:         decision.Quantity.String(),
			PositionId:       decision.PositionID,
			LotSequence:      decision.LotSequence,
			State:            longgrid.Dispatched,
			ExecutedQuantity: "0",
			ExecutedNotional: "0",
		}
		if err := gridDB(ctx, tx).Model(&model.LongGridAction{}).Create(action).Error; err != nil {
			return err
		}
		if stop {
			state.StopActionId = proto.Int64(action.Id)
		} else {
			state.PendingActionId = proto.Int64(action.Id)
		}
		state.NextSequence++
		return saveGridCursor(ctx, tx, state)
	})
	return action, err
}

func (r *Runtime) gridOpeningRisk(ctx context.Context, tx *gorm.DB, snapshot *common.VenueSnapshot, decision longgrid.Decision) error {
	profile := r.cfg.Venue.Profiles[0]
	var budget commonmodel.TradingAccountPolicy
	if err := gridDB(ctx, tx).Model(&commonmodel.TradingAccountPolicy{}).Where("account_id = ? AND enabled AND delete_at IS NULL", AccountID(profile.UserId, profile.FundId)).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&budget).Error; err != nil {
		return err
	}
	if budget.Leverage != profile.Leverage || budget.Symbol != profile.Symbol {
		return ErrRiskLimit
	}
	limit, e1 := domain.ParseLedgerDecimal(budget.MaximumOrderNotional, false, false)
	maximum, e2 := domain.ParseLedgerDecimal(budget.MaximumNotional, false, false)
	available, e3 := native.Value(snapshot.Available, false, true)
	fee, e4 := native.Value(snapshot.GetMarket().GetWorstFeeRate(), false, true)
	held, _, _, e5 := longgrid.Position(snapshot)
	mark, e6 := native.Value(snapshot.GetMarket().GetMarkPrice(), false, false)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil {
		return ErrRiskLimit
	}
	for _, order := range snapshot.Orders {
		if native.Active(order.Status) {
			return ErrRiskLimit
		}
	}
	notional := decision.Price.Mul(decision.Quantity)
	margin, remainder := notional.QuoRem(decimal.NewFromInt(int64(profile.Leverage)), 18)
	if !remainder.IsZero() {
		margin = margin.Add(decimal.New(1, -18))
	}
	if notional.GreaterThan(limit) || held.Mul(mark).Add(notional).GreaterThan(maximum) || margin.Add(notional.Mul(fee)).GreaterThan(available) {
		return ErrRiskLimit
	}
	return nil
}
