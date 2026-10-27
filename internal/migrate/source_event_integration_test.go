//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
	"github.com/hmlax/muskdeer/internal/usecase"
)

func TestCoreFillAndSourceOutboxCommitOrRollBackTogether(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	intent, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("controlled fill event insert failure")
	active := true
	const callbackName = "muskdeer:test:reject-source-event"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*modelv1.OutboxEvent); ok && event.EventType == domain.EventTradeFilled && active {
			tx.Error = failure
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	request := domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       intent.ClientOrderID,
		TradeID:             "atomic-source-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}
	if _, err := core.Fill(ctx, request); !errors.Is(err, failure) {
		t.Fatalf("fill effect ignored failed source publication: %v", err)
	}
	assertCoreBalance(ctx, t, core, request.AccountID, "USDT", "799.8", "200.2")
	assertCoreBalance(ctx, t, core, request.AccountID, "BTC", "10", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	unchanged, err := core.Find(ctx, request.AccountID, request.ClientOrderID)
	if err != nil || unchanged.State != domain.OrderStateLive || !unchanged.FilledQuantity.IsZero() {
		t.Fatalf("failed outbox retained a partial effect: %+v, %v", unchanged, err)
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).First(&risk).Error; err != nil || !risk.Reconciled {
		t.Fatalf("rolled-back source event changed risk: %v, %v", &risk, err)
	}
	active = false
	fill, err := core.Fill(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := core.Fill(ctx, request)
	if err != nil || duplicate.ID != fill.ID {
		t.Fatalf("source retry repeated trade effect: %+v, %v", duplicate, err)
	}
	var events []*modelv1.OutboxEvent
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Where("event_type = ?", domain.EventTradeFilled).Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("natural trade produced duplicate events: %d, %v", len(events), err)
	}
	notice, err := eventwire.Fill(ctx, events[0].Event)
	if err != nil || notice.Fact.SourceSequence != fill.ID || notice.Fact.IntentID != intent.ID || events[0].IntentId != intent.ID {
		t.Fatalf("source event lost its persisted identity: %+v, %v", notice, err)
	}
}

func TestFillReplayFinishesOMSAfterPortfolioAlreadyCommitted(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	failure := errors.New("controlled reconciliation interruption")
	recovery := &interruptedReconciler{
		IntentService: intentServiceFixture(t, intents, core),
		failure:       failure,
	}
	handler, err := usecase.NewFillConsumer(portfolio, recovery)
	if err != nil {
		t.Fatal(err)
	}
	if err := handler.Apply(ctx, portfolioNotice(ctx, t, buy)); !errors.Is(err, failure) {
		t.Fatalf("consumer acknowledged an unfinished OMS effect: %v", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	for _, event := range []*commonv1.ExecutionEvent{buy, buy, sell} {
		if err := handler.Apply(ctx, portfolioNotice(ctx, t, event)); err != nil {
			t.Fatal(err)
		}
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 2)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	projection, err := portfolio.Get(ctx, submit.Input.AccountID)
	if err != nil || !projection.Reconciled || !projection.Values.CashQuote.Equal(decimal.RequireFromString("1001.8")) {
		t.Fatalf("resumed consumer changed completed accounting: %+v, %v", projection, err)
	}
}

type interruptedReconciler struct {
	domain.IntentService
	failure error
}

func (r *interruptedReconciler) Recover(ctx context.Context, accountID string, intentID int64) (domain.IntentSnapshot, error) {
	if r.failure != nil {
		failure := r.failure
		r.failure = nil
		return domain.IntentSnapshot{}, failure
	}
	return r.IntentService.Recover(ctx, accountID, intentID)
}
