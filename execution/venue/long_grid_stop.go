package venue

import (
	"context"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/longgrid"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func (r *Runtime) rearmGrid(ctx context.Context, observed *model.LongGridRuntime, control *strategy.StrategyInstance, snapshot *common.VenueSnapshot) (*model.LongGridRuntime, error) {
	reset := observed.Revision != r.cfg.Revision || observed.State == longgrid.Stopped && control.DesiredRunning && control.RequestedConfigVersion > observed.HaltedControlVersion
	if !reset {
		return observed, nil
	}
	quantity, _, _, err := longgrid.Position(snapshot)
	if err != nil || quantity.IsPositive() {
		return nil, longgrid.ErrInvalid
	}
	var result *model.LongGridRuntime
	err = gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Transaction(func(tx *gorm.DB) error {
		state, err := lockGridCursor(ctx, tx, r.cfg.InstanceID)
		if err != nil {
			return err
		}
		if state.NextSequence != observed.NextSequence || state.PendingActionId != nil || state.StopActionId != nil || state.State == longgrid.Stopping {
			return longgrid.ErrInvalid
		}
		state.Revision = r.cfg.Revision
		state.State = longgrid.Running
		state.Started = false
		state.NextBuyPrice = "0"
		state.Lots = &common.LongGridLots{}
		if err := saveGridCursor(ctx, tx, state); err != nil {
			return err
		}
		result = state
		return nil
	})
	return result, err
}

func (r *Runtime) runGridStop(ctx context.Context, state *model.LongGridRuntime, snapshot *common.VenueSnapshot, decision longgrid.Decision, client gateway.LongGridClient) (*pb.LongGridResult, error) {
	if state.State != longgrid.Stopping {
		if err := gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Where("id = ? AND revision = ?", state.Id, r.cfg.Revision).Update("State", longgrid.Stopping).Error; err != nil {
			return nil, err
		}
		state.State = longgrid.Stopping
	}
	pending, err := r.cancelGridOrders(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	if pending || state.StopActionId != nil {
		return &pb.LongGridResult{State: longgrid.Stopping}, nil
	}
	quantity, _, _, err := longgrid.Position(snapshot)
	if err != nil {
		return nil, err
	}
	if quantity.IsZero() {
		if state.PendingActionId != nil {
			return &pb.LongGridResult{State: longgrid.Stopping}, nil
		}
		if err := r.haltGrid(ctx, state); err != nil {
			return nil, err
		}
		return &pb.LongGridResult{
			State:  longgrid.Stopped,
			Action: longgrid.StopLoss,
		}, nil
	}
	if decision.Kind != longgrid.StopLoss {
		return nil, longgrid.ErrInvalid
	}
	action, err := r.claimGridAction(ctx, state, snapshot, nil, decision)
	if err != nil {
		return nil, err
	}
	return r.sendGridAction(ctx, action, client, snapshot.ValidUntilMs)
}

func (r *Runtime) haltGrid(ctx context.Context, observed *model.LongGridRuntime) error {
	return gridDB(ctx, r.db).Model(&model.LongGridRuntime{}).Transaction(func(tx *gorm.DB) error {
		control := &strategy.StrategyInstance{}
		if err := gridDB(ctx, tx).Model(&strategy.StrategyInstance{}).Where("id = ? AND revision = ?", r.cfg.InstanceID, r.cfg.Revision).Clauses(clause.Locking{Strength: "UPDATE"}).Take(control).Error; err != nil {
			return err
		}
		state, err := lockGridCursor(ctx, tx, r.cfg.InstanceID)
		if err != nil {
			return err
		}
		if state.State != longgrid.Stopping || state.PendingActionId != nil || state.StopActionId != nil || state.NextSequence != observed.NextSequence {
			return longgrid.ErrInvalid
		}
		if control.DesiredRunning {
			stopped, err := instanceconfig.ManageConfigured(ctx, tx, instanceconfig.ManagementWrite{
				Operation:              instanceconfig.ManagementStop,
				Subject:                longgrid.StopLoss,
				RequestID:              "long-grid-stop-" + strconv.FormatInt(control.Id, 10) + "-" + strconv.FormatInt(state.NextSequence, 10),
				ID:                     control.Id,
				ExpectedRevision:       control.Revision,
				ExpectedControlVersion: control.RequestedConfigVersion,
			})
			if err != nil {
				return err
			}
			control = stopped
		}
		state.State = longgrid.Stopped
		state.Lots = &common.LongGridLots{}
		state.HaltedControlVersion = control.RequestedConfigVersion
		return saveGridCursor(ctx, tx, state)
	})
}
