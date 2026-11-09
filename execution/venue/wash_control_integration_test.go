//go:build integration

package venue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

func TestConfiguredWashLoadsAndControlsWithoutHistoryQueries(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	forbidden := denyWashHistory(t, setup.db)
	row, err := instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Subject:   "wash-test",
		RequestID: "wash-create-request-0001",
		Input: &common.StrategyInstanceInput{
			InstanceKey:      "direct-wash",
			TemplateId:       strategyregistry.WashTradeTemplateID,
			TemplateRevision: 1,
			AccountId:        setup.instance.AccountId,
			InstrumentId:     setup.instance.InstrumentId,
			MarketDataSource: native.Source,
			Parameters: &common.StrategyInstanceParameters{
				SchemaVersion: 1,
				Execution:     proto.CloneOf(setup.instance.Parameters.Execution),
				WashTrade: &common.WashTradeParameters{
					DevelopmentOnly:  true,
					Mode:             common.WashTradeMode_WASH_TRADE_MODE_AGG_TRADE,
					IntervalMs:       100,
					Quantity:         "0.01",
					QuantityFraction: "0.1",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Retired digests can disagree with current parameters. Preserve these audit
	// columns in storage without treating them as execution requirements.
	if err := setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Updates(&strategy.StrategyInstance{
		TapePolicyDigest:     "historical-policy",
		TapeParametersDigest: "historical-parameters",
		Revision:             row.Revision + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	row.Revision++
	loaded, err := instanceconfig.LoadConfigured(ctx, setup.db)
	if err != nil {
		t.Fatal(err)
	}
	var runtime *Runtime
	for _, instance := range loaded {
		if instance.ID == row.Id {
			settings, err := instance.Settings()
			if err != nil {
				t.Fatal(err)
			}
			runtime = &Runtime{cfg: settings}
		}
	}
	if runtime == nil {
		t.Fatal("configured wash missing")
	}
	children := []*Runtime{runtime}
	if err := refreshWashControls(ctx, setup.db, children); err != nil || runtime.washControl.Load().active {
		t.Fatalf("new instance active: %v", err)
	}
	command := func(operation, request string) {
		t.Helper()
		row, err = instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
			Operation:              operation,
			Subject:                "wash-test",
			RequestID:              request,
			ID:                     row.Id,
			ExpectedRevision:       row.Revision,
			ExpectedControlVersion: row.RequestedConfigVersion,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := refreshWashControls(ctx, setup.db, children); err != nil {
			t.Fatal(err)
		}
	}
	command(instanceconfig.ManagementStart, "wash-start-request-0001")
	if !runtime.washControl.Load().active {
		t.Fatal("start not refreshed")
	}
	command(instanceconfig.ManagementStop, "wash-stop-request-00001")
	if runtime.washControl.Load().active {
		t.Fatal("stop not refreshed")
	}
	parameters := proto.CloneOf(row.Parameters)
	parameters.WashTrade.Quantity = "0.02"
	row, err = instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementUpdate,
		Subject:                "wash-test",
		RequestID:              "wash-update-request-0001",
		ID:                     row.Id,
		ExpectedRevision:       row.Revision,
		ExpectedControlVersion: row.RequestedConfigVersion,
		Input:                  &common.StrategyInstanceInput{Parameters: parameters},
	})
	if err != nil {
		t.Fatal(err)
	}
	command(instanceconfig.ManagementStart, "wash-start-request-0002")
	if runtime.washControl.Load().active {
		t.Fatal("old revision still active after parameter update")
	}
	var stored strategy.StrategyInstance
	if err := setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", row.Id).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TapePolicyDigest != "historical-policy" || stored.TapeParametersDigest != "historical-parameters" {
		t.Fatal("old metadata overwritten")
	}
	if _, err := instanceconfig.LoadConfigured(ctx, setup.db); err != nil {
		t.Fatal(err)
	}
	if forbidden.Load() != 0 {
		t.Fatalf("history storage accessed %d times", forbidden.Load())
	}
}

func denyWashHistory(t *testing.T, db *gorm.DB) *atomic.Int64 {
	t.Helper()
	count := &atomic.Int64{}
	callback := func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "synthetic_tape_intents" {
			count.Add(1)
			_ = tx.AddError(errors.New("WashTrade history access is forbidden"))
		}
	}
	if err := db.Callback().Query().Before("gorm:query").Register("test:deny_wash_history", callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("test:deny_wash_history", callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("test:deny_wash_history", callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register("test:deny_wash_history", callback); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, err := range []error{db.Callback().Query().Remove("test:deny_wash_history"), db.Callback().Row().Remove("test:deny_wash_history"), db.Callback().Create().Remove("test:deny_wash_history"), db.Callback().Update().Remove("test:deny_wash_history")} {
			if err != nil {
				t.Error(err)
			}
		}
	})
	return count
}
