//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/himalaya"
)

func TestSyntheticTapeReplyLossAndConcurrentRetriesConsumeOneSlot(t *testing.T) {
	db, store, lease, cfg, plan := syntheticDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var calls atomic.Int64
	send := func(ctx context.Context, input *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
		calls.Add(1)
		var row modelv1.SyntheticTapeIntent
		if err := db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Where("run_id = ?", plan.RunId).First(&row).Error; err != nil {
			return nil, err
		}
		if row.Result.GetState() != native.SyntheticUnknown || input.Quantity != "0.01" || input.Side != int32(pb.Side_SIDE_BUY) {
			t.Error("synthetic effect preceded its durable slot or changed its SDK payload")
		}
		return nil, errors.New("injected synthetic reply loss")
	}
	var running sync.WaitGroup
	results := make(chan error, 4)
	for range 4 {
		running.Go(func() {
			result, err := store.PublishSynthetic(ctx, lease, cfg, plan, send)
			if err == nil && result.GetState() != native.SyntheticUnknown {
				err = errors.New("reply loss did not remain unknown")
			}
			results <- err
		})
	}
	running.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("PublishSynthetic(concurrent retry) = %d effects, want 1", calls.Load())
	}
	var budget modelv1.HimalayaBudget
	if err := db.WithContext(ctx).Model(&modelv1.HimalayaBudget{}).First(&budget).Error; err != nil {
		t.Fatal(err)
	}
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if err != nil || !reserved.IsZero() {
		t.Fatalf("synthetic publication touched financial reservations: %s", budget.ReservedNotional)
	}
}

func TestSyntheticTapeQuotaIntervalPolicyAndEpochRemainEnforced(t *testing.T) {
	db, store, lease, cfg, plan := syntheticDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	calls := 0
	send := func(_ context.Context, input *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
		calls++
		for _, account := range []string{lease.Key.AccountID, plan.Strategy.AccountId, plan.Book.Publisher.AccountId} {
			var held modelv1.ExecutionShardLease
			err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", account).
				Clauses(clause.Locking{
					Strength: "UPDATE",
					Options:  "NOWAIT",
				}).First(&held).Error
			var state interface{ SQLState() string }
			if !errors.As(err, &state) || state.SQLState() != "55P03" {
				t.Errorf("synthetic final authority lock = %v, want PostgreSQL lock_not_available", err)
			}
		}
		return &pb.WashTradeResponse{
			TradeId:     "synthetic-test-1",
			Price:       input.Price,
			Quantity:    input.Quantity,
			TimestampMs: uint64(time.Now().UnixMilli()),
		}, nil
	}
	result, err := store.PublishSynthetic(ctx, lease, cfg, plan, send)
	if err != nil || result.GetState() != native.SyntheticPublished {
		t.Fatalf("PublishSynthetic(first) = %v, %v", result, err)
	}
	restarted, err := himalaya.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := restarted.PublishSynthetic(ctx, lease, cfg, plan, send); err != nil || !replay.GetReplayed() || calls != 1 {
		t.Fatalf("PublishSynthetic(restart replay) calls=%d replayed=%t err=%v", calls, replay.GetReplayed(), err)
	}
	changed := proto.CloneOf(cfg)
	changed.SyntheticTape.MaximumEvents++
	if _, err := store.PublishSynthetic(ctx, lease, changed, plan, send); err == nil || calls != 1 {
		t.Fatalf("PublishSynthetic(policy mutation) calls=%d err=%v", calls, err)
	}
	next, err := native.SyntheticPlan(cfg, plan.Symbol, 1, plan.Book, plan.Strategy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishSynthetic(ctx, lease, cfg, next, send); err == nil || calls != 1 {
		t.Fatalf("PublishSynthetic(early next slot) calls=%d err=%v", calls, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Where("run_id = ?", plan.RunId).UpdateColumn("create_at", time.Now().Add(-time.Minute).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishSynthetic(ctx, lease, cfg, next, send); err == nil || calls != 1 {
		t.Fatalf("PublishSynthetic(total notional exhausted) calls=%d err=%v", calls, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", plan.Book.Publisher.AccountId).UpdateColumn("epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	if replay, err := store.PublishSynthetic(ctx, lease, cfg, plan, send); err != nil || !replay.GetReplayed() || calls != 1 {
		t.Fatalf("cached receipt acquired publication authority: calls=%d err=%v", calls, err)
	}
	stale := proto.CloneOf(next)
	stale.Book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	if _, err := store.PublishSynthetic(ctx, lease, cfg, stale, send); !errors.Is(err, domain.ErrLeaseLost) || calls != 1 {
		t.Fatalf("PublishSynthetic(stale source epoch) calls=%d err=%v", calls, err)
	}
}

func syntheticDispatchFixture(t *testing.T) (*gorm.DB, *himalaya.Store, domain.LeaseToken, *configv1.HimalayaRuntime, *commonv1.SyntheticTapePlan) {
	t.Helper()
	db, store, fundLease, request := sdkAdmissionFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	authority := liveAuthorityFixture(ctx, t, db, fundLease, request.Symbol)
	authority.Book.Bids = []*commonv1.HimalayaLevel{{
		Price:    "99.9",
		Quantity: "1",
	}}
	authority.Book.Asks = []*commonv1.HimalayaLevel{{
		Price:    "100.1",
		Quantity: "1",
	}}
	cfg := &configv1.HimalayaRuntime{
		Profiles: []*configv1.HimalayaProfile{{
			Symbol:       request.Symbol,
			PriceTick:    "0.1",
			QuantityStep: "0.001",
		}},
		SyntheticTape: &configv1.SyntheticTape{
			Enabled:         true,
			DevelopmentOnly: true,
			RunId:           "test-tape-1",
			Interval:        durationpb.New(time.Second),
			MaximumEvents:   2,
			Profiles: []*configv1.SyntheticTapeProfile{{
				Symbol:          request.Symbol,
				Quantity:        "0.01",
				MaximumNotional: "1.5",
			}},
		},
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	acquire := func(account string) domain.LeaseToken {
		t.Helper()
		lease, err := leases.Acquire(ctx, domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: account,
		}, fundLease.OwnerID, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return lease
	}
	execution := acquire(native.SyntheticExecutionKey(cfg.SyntheticTape.RunId, request.Symbol))
	strategy := acquire(native.SyntheticStrategyKey(cfg.SyntheticTape.RunId, request.Symbol))
	// Publish the fixture snapshot after database setup, as the live market
	// worker does. Setup time must not consume the source's freshness window.
	authority.Book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
	plan, err := native.SyntheticPlan(cfg, request.Symbol, 0, authority.Book, native.Authority(strategy))
	if err != nil {
		t.Fatal(err)
	}
	return db, store, execution, cfg, plan
}
