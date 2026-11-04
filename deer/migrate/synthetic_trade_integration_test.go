//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/protobuf/proto"
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

type aggregateDispatchState struct {
	db        *gorm.DB
	store     *himalaya.Store
	execution domain.LeaseToken
	source    domain.LeaseToken
	cfg       *configv1.HimalayaRuntime
	base      *commonv1.SyntheticTapePlan
}

func aggregateDispatchFixture(t *testing.T) *aggregateDispatchState {
	t.Helper()
	db, store, execution, cfg, base := syntheticDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	source, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.TradeKey(base.Symbol),
	}, execution.OwnerID, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cfg.QuantityFraction = "0.1"
	cfg.SyntheticTape.Mode = configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE
	cfg.SyntheticTape.MaximumEvents = 3
	cfg.SyntheticTape.Profiles[0].MaximumNotional = "3"
	return &aggregateDispatchState{
		db:        db,
		store:     store,
		execution: execution,
		source:    source,
		cfg:       cfg,
		base:      base,
	}
}

func (s *aggregateDispatchState) plan(t *testing.T, sequence uint32, id int64) *commonv1.SyntheticTapePlan {
	t.Helper()
	now := time.Now()
	input := proto.CloneOf(s.base)
	input.Sequence = sequence
	input.Book.ValidUntilMs = now.Add(native.BookAge).UnixMilli()
	input.SourceTrade = &commonv1.HimalayaTrade{
		Symbol:       input.Symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		EventId:      strconv.FormatInt(id, 10),
		Side:         string(domain.SideSell),
		Price:        "100.43",
		Quantity:     "0.07",
		EventAtMs:    now.UnixMilli(),
		TradeAtMs:    now.UnixMilli(),
		ReceivedAtMs: now.UnixMilli(),
		ValidUntilMs: now.Add(native.TradeAge).UnixMilli(),
		Publisher:    native.Authority(s.source),
	}
	plan, err := native.SyntheticSourcePlan(s.cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestSyntheticTradeUnknownAndConcurrentNextSlotsCannotReuseAnEvent(t *testing.T) {
	s := aggregateDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var calls atomic.Int64
	send := func(_ context.Context, input *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
		call := calls.Add(1)
		if input.Side != int32(pb.Side_SIDE_SELL) || input.Price != "100.5" || input.Quantity != "0.007" {
			t.Errorf("aggregate SDK payload = %v, want source-derived SELL/100.5/0.007", input)
		}
		if call == 1 {
			return nil, errors.New("injected aggregate reply loss")
		}
		return &pb.WashTradeResponse{
			TradeId:     "aggregate-test-" + strconv.FormatInt(call, 10),
			Price:       input.Price,
			Quantity:    input.Quantity,
			TimestampMs: uint64(time.Now().UnixMilli()),
		}, nil
	}
	first, err := s.store.PublishSynthetic(ctx, s.execution, s.cfg, s.plan(t, 0, 42), send)
	if err != nil || first.GetState() != native.SyntheticUnknown || first.GetSourceTrade().GetEventId() != "42" {
		t.Fatalf("aggregate reply loss = %v, %v, want unknown event 42", first, err)
	}
	if err := s.db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Where("run_id = ?", s.base.RunId).UpdateColumn("create_at", time.Now().Add(-2*time.Second).UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PublishSynthetic(ctx, s.execution, s.cfg, s.plan(t, 1, 42), send); !errors.Is(err, himalaya.ErrInvalidSubmit) || calls.Load() != 1 {
		t.Fatalf("consumed event admitted a second slot: calls=%d err=%v", calls.Load(), err)
	}
	restarted, err := himalaya.NewStore(s.db)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := restarted.PublishSynthetic(ctx, s.execution, s.cfg, s.plan(t, 0, 43), send); err != nil || !replay.GetReplayed() || replay.GetSourceTrade().GetEventId() != "42" || calls.Load() != 1 {
		t.Fatalf("aggregate restart replay = %v, %v calls=%d, want original event without SDK effect", replay, err, calls.Load())
	}
	plans := []*commonv1.SyntheticTapePlan{s.plan(t, 1, 43), s.plan(t, 2, 43)}
	var running sync.WaitGroup
	results := make(chan error, 2)
	for _, plan := range plans {
		running.Go(func() { _, err := restarted.PublishSynthetic(ctx, s.execution, s.cfg, plan, send); results <- err })
	}
	running.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 || calls.Load() != 2 {
		t.Fatalf("same aggregate event across concurrent slots: successes=%d effects=%d, want 1/2 total", successes, calls.Load())
	}
	var rows []*modelv1.SyntheticTapeIntent
	if err := s.db.WithContext(ctx).Model(&modelv1.SyntheticTapeIntent{}).Order("sequence").Find(&rows).Error; err != nil || len(rows) != 2 || rows[0].Plan.GetSourceTrade().GetEventId() != "42" || rows[1].Plan.GetSourceTrade().GetEventId() != "43" {
		t.Fatalf("durable aggregate cursor = %v, %v, want two typed events", rows, err)
	}
	budget, err := s.store.Budget(ctx, native.ScopeKey(10001, 10001))
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if err != nil || !reserved.IsZero() {
		t.Fatalf("aggregate publication changed financial reservation %s: %v", budget.ReservedNotional, err)
	}
}

func TestSyntheticTradePublisherEpochAndFinalAuthorityLockRemainRequired(t *testing.T) {
	s := aggregateDispatchFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	calls := 0
	send := func(_ context.Context, input *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
		calls++
		var held modelv1.ExecutionShardLease
		err := s.db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", s.source.Key.AccountID).Clauses(clause.Locking{
			Strength: "UPDATE",
			Options:  "NOWAIT",
		}).First(&held).Error
		var state interface{ SQLState() string }
		if !errors.As(err, &state) || state.SQLState() != "55P03" {
			t.Errorf("aggregate source was not locked through SDK send: %v", err)
		}
		return &pb.WashTradeResponse{
			TradeId:     "aggregate-lock-test",
			Price:       input.Price,
			Quantity:    input.Quantity,
			TimestampMs: uint64(time.Now().UnixMilli()),
		}, nil
	}
	if result, err := s.store.PublishSynthetic(ctx, s.execution, s.cfg, s.plan(t, 0, 42), send); err != nil || result.GetState() != native.SyntheticPublished {
		t.Fatalf("owned aggregate publish=%v, %v", result, err)
	}
	if err := s.db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).Where("account_id = ?", s.source.Key.AccountID).UpdateColumn("epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.PublishSynthetic(ctx, s.execution, s.cfg, s.plan(t, 1, 43), send); !errors.Is(err, domain.ErrLeaseLost) || calls != 1 {
		t.Fatalf("stale aggregate publisher effects=%d err=%v, want one effect and lease lost", calls, err)
	}
}
