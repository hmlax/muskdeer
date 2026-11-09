//go:build integration

package venue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/execution/adapters/himalaya"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func TestMatchingSyntheticConcurrentUnknownSlotIsNeverResent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	wash, err := instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Subject:   "matching-test",
		RequestID: "matching-test-create-wash",
		Input: &common.StrategyInstanceInput{
			InstanceKey:      "matching-wash",
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
					RunId:            "matching-wash",
					IntervalMs:       100,
					Quantity:         "0.01",
					MaximumNotional:  "0",
					QuantityFraction: "0.1",
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wash, err = instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementStart,
		Subject:                "matching-test",
		RequestID:              "matching-test-start-wash",
		ID:                     wash.Id,
		ExpectedRevision:       wash.Revision,
		ExpectedControlVersion: wash.RequestedConfigVersion,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := instanceconfig.Load(ctx, setup.db)
	if err != nil {
		t.Fatal(err)
	}
	var instance *instanceconfig.Instance
	for _, row := range loaded {
		if row.ID == wash.Id {
			instance = row
		}
	}
	if instance == nil {
		t.Fatal("wash instance not loaded")
	}
	settings, err := instance.Settings()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	plan, err := native.SyntheticSourcePlan(settings.Venue, &common.SyntheticTapePlan{
		RunId:              wash.Parameters.WashTrade.RunId,
		Symbol:             instance.Profile.Symbol,
		StrategyInstanceId: wash.Id,
		StrategyRevision:   wash.Revision,
		Strategy:           &common.VenueAuthority{AccountId: native.SyntheticStrategyKey(wash.Parameters.WashTrade.RunId, instance.Profile.Symbol)},
		Book: &common.VenueBook{
			Symbol:       instance.Profile.Symbol,
			Source:       native.Source,
			Segment:      native.Segment,
			ValidUntilMs: now + native.BookAge.Milliseconds(),
			Publisher:    &common.VenueAuthority{AccountId: native.MarketKey(instance.Profile.Symbol)},
		},
		SourceTrade: &common.VenueTrade{
			Symbol:       instance.Profile.Symbol,
			Source:       native.Source,
			Segment:      native.Segment,
			EventId:      "1",
			Side:         string(domain.SideBuy),
			Price:        "100",
			Quantity:     "1",
			EventAtMs:    now,
			TradeAtMs:    now,
			ReceivedAtMs: now,
			ValidUntilMs: now + native.TradeAge.Milliseconds(),
			Publisher:    &common.VenueAuthority{AccountId: native.TradeKey(instance.Profile.Symbol)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewMatchingStore(setup.db, himalaya.Factory{})
	if err != nil {
		t.Fatal(err)
	}
	forbidden := denyMatchingQueries(t, setup.db)
	var calls atomic.Int64
	send := func(context.Context, *gateway.SyntheticRequest) (*gateway.SyntheticResult, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	}
	results, failures := make(chan *common.SyntheticTapeResult, 2), make(chan error, 2)
	for range 2 {
		go func() {
			result, err := store.PublishSynthetic(ctx, domain.LeaseToken{Key: domain.LeaseKey{AccountID: native.SyntheticExecutionKey(plan.RunId, plan.Symbol)}}, settings.Venue, plan, send)
			results <- result
			failures <- err
		}()
	}
	replayed := 0
	for range 2 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
		result := <-results
		if result.GetState() != native.SyntheticUnknown {
			t.Fatalf("lost reply state=%s", result.GetState())
		}
		if result.Replayed {
			replayed++
		}
	}
	if forbidden.Load() != 0 {
		t.Fatalf("lease/reservation queries = %d", forbidden.Load())
	}
	if calls.Load() != 1 || replayed != 1 {
		t.Fatalf("concurrent synthetic sends=%d replays=%d, want 1/1", calls.Load(), replayed)
	}
	restarted, err := instanceconfig.Load(ctx, setup.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range restarted {
		if row.ID == wash.Id && row.ResumeSequence != 1 {
			t.Fatalf("restart sequence=%d, want consumed UNKNOWN slot 1", row.ResumeSequence)
		}
	}
}
