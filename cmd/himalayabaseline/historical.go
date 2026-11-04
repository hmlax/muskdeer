package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

const messageHistoricalResult = "%s: historical_intents=%d risk_retained=true applied=%t\n"

func (r *baselineRunner) historical(ctx context.Context, batch *commonv1.HistoricalIntentApprovalBatch, report string) error {
	rows := make([]*executionmodelv1.VenueOrderIntent, 0)
	for _, request := range batch.Requests {
		profile, err := native.Profile(r.cfg, request.GetScope())
		if err != nil {
			return err
		}
		// Even preview owns the execution lease: another writer must not race the
		// two observations or the finite list of approvals being reviewed.
		lease, err := r.acquire(ctx, request.Scope)
		if err != nil {
			return err
		}
		snapshot, err := stableSnapshot(ctx, r.client, himalaya.Scope{
			UserID: request.Scope.UserId,
			FundID: request.Scope.FundId,
			Symbol: request.Scope.Symbol,
		})
		if err != nil {
			return err
		}
		if snapshot.Market.PriceTick != profile.PriceTick || snapshot.Market.QuantityStep != profile.QuantityStep {
			return himalaya.ErrInvalidSnapshot
		}
		if r.apply {
			if err := r.store.ApproveHistoricalIntents(ctx, lease, request, snapshot); err != nil {
				return err
			}
		} else if err := r.store.AssessHistoricalIntents(ctx, lease, request, snapshot); err != nil {
			return err
		}
		var current []*executionmodelv1.VenueOrderIntent
		if err := r.db.WithContext(ctx).Model(&executionmodelv1.VenueOrderIntent{}).
			Where("account_id = ? AND id IN ?", lease.Key.AccountID, request.IntentIds).Order("id").Find(&current).Error; err != nil {
			return err
		}
		if len(current) != len(request.IntentIds) {
			return himalaya.ErrInvalidSubmit
		}
		rows = append(rows, current...)
		fmt.Printf(messageHistoricalResult, profile.Symbol, len(current), r.apply)
	}
	if report != "" {
		encoded, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(report, encoded, 0600)
	}
	return nil
}
