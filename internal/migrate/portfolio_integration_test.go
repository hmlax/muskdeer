//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/eventwire"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestPortfolioInboxNaturalKeysAndOutOfOrderRebuild(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	buyEvent, sellEvent := portfolioTrades(ctx, t, core, intents, submit)
	sell := portfolioNotice(ctx, t, sellEvent)
	partial, err := portfolio.Apply(ctx, sell)
	if err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatalf("out-of-order incomplete projection = %+v, %v", partial, err)
	}
	if _, err := portfolio.Apply(ctx, sell); err != nil {
		t.Fatal(err)
	}
	buy := portfolioNotice(ctx, t, buyEvent)
	complete, err := portfolio.Apply(ctx, buy)
	if err != nil || !complete.Reconciled || !complete.Values.Quantity.Equal(decimal.NewFromInt(10)) || !complete.Values.CashQuote.Equal(decimal.RequireFromString("1001.8")) || !complete.Values.FeesQuote.Equal(decimal.RequireFromString("0.2")) {
		t.Fatalf("complete exact projection = %+v, %v", complete, err)
	}
	totalPnl := complete.Values.RealizedPnlQuote.Add(decimal.NewFromInt(100).Mul(complete.Values.Quantity).Sub(complete.Values.CostQuote))
	if !totalPnl.Equal(decimal.RequireFromString("1.8")) {
		t.Fatalf("fees or average cost counted twice: %s", totalPnl)
	}
	alias := proto.Clone(sellEvent).(*commonv1.ExecutionEvent)
	alias.EventId += ":redelivered"
	duplicate, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, alias))
	if err != nil || !duplicate.Values.CostQuote.Equal(complete.Values.CostQuote) || !duplicate.Values.RealizedPnlQuote.Equal(complete.Values.RealizedPnlQuote) {
		t.Fatalf("new delivery ID repeated a natural trade: %+v, %v", duplicate, err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 3)
	conflicting := proto.Clone(buyEvent).(*commonv1.ExecutionEvent)
	conflicting.RequestId = "changed-envelope"
	if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, conflicting)); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("same event ID changed its fingerprint: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", submit.Input.AccountID).UpdateColumn("quantity", "9").Error; err != nil {
		t.Fatal(err)
	}
	if checked, err := portfolio.Get(ctx, submit.Input.AccountID); err != nil || checked.Reconciled {
		t.Fatalf("corrupt projection was called reconciled: %+v, %v", checked, err)
	}
	rebuilt, err := portfolio.Rebuild(ctx, submit.Input.AccountID)
	if err != nil || !rebuilt.Reconciled || !rebuilt.Values.Quantity.Equal(complete.Values.Quantity) || !rebuilt.Values.CostQuote.Equal(complete.Values.CostQuote) || !rebuilt.Values.RealizedPnlQuote.Equal(complete.Values.RealizedPnlQuote) {
		t.Fatalf("fact rebuild differs from original: %+v, %v", rebuilt, err)
	}
	err = db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", submit.Input.AccountID).UpdateColumn("opening_quantity", "9").Error
	assertDatabaseCode(t, err, "23514")
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).First(&risk).Error; err != nil || risk.Reconciled {
		t.Fatalf("portfolio silently reopened a risk grant: %v, %v", &risk, err)
	}
}

func TestPortfolioProjectionFailureRollsBackInboxAndFact(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	buyEvent, sellEvent := portfolioTrades(ctx, t, core, intents, submit)
	if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sellEvent)); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("controlled portfolio update failure")
	active := true
	const callbackName = "muskdeer:test:reject-portfolio-write"
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.PortfolioAccount); ok && active {
			tx.Error = injected
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	if _, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, buyEvent)); !errors.Is(err, injected) {
		t.Fatalf("projection failure lost its cause: %v", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	active = false
	if complete, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, buyEvent)); err != nil || !complete.Reconciled {
		t.Fatalf("rolled-back event could not recover: %+v, %v", complete, err)
	}
}

func portfolioFixture(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit) domain.PortfolioRepository {
	t.Helper()
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := portfolio.Initialize(ctx, domain.PortfolioOpening{
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		Quantity:     decimal.NewFromInt(10),
		CashQuote:    decimal.NewFromInt(1000),
		CostQuote:    decimal.NewFromInt(1000),
	}); err != nil {
		t.Fatal(err)
	}
	return portfolio
}

func portfolioTrades(ctx context.Context, t *testing.T, core domain.Simulator, intents domain.IntentExecutionRepository, original domain.GatewaySubmit) (*commonv1.ExecutionEvent, *commonv1.ExecutionEvent) {
	t.Helper()
	orders := intentServiceFixture(t, intents, core)
	buy := original.Input
	buy.Price = decimal.NewFromInt(99)
	buy.Quantity = decimal.NewFromInt(1)
	buy.IdempotencyKey = "portfolio-buy"
	buyIntent, err := orders.Submit(ctx, buy, original.ExecutionAuthority)
	if err != nil || buyIntent.State != domain.OrderStateLive {
		t.Fatalf("portfolio source buy = %+v, %v", buyIntent, err)
	}
	sell := buy
	sell.Side = domain.SideSell
	sell.Price = decimal.NewFromInt(101)
	sell.IdempotencyKey = "portfolio-sell"
	sellIntent, err := orders.Submit(ctx, sell, original.ExecutionAuthority)
	if err != nil || sellIntent.State != domain.OrderStateLive {
		t.Fatalf("portfolio source sell = %+v, %v", sellIntent, err)
	}
	buyFill, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           buy.AccountID,
		ClientOrderID:       buyIntent.ClientOrderID,
		TradeID:             "portfolio-buy-fill",
		CounterpartySubject: "external-liquidity",
		Price:               buy.Price,
		Quantity:            buy.Quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	sellFill, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           sell.AccountID,
		ClientOrderID:       sellIntent.ClientOrderID,
		TradeID:             "portfolio-sell-fill",
		CounterpartySubject: "external-liquidity",
		Price:               sell.Price,
		Quantity:            sell.Quantity,
	})
	if err != nil {
		t.Fatal(err)
	}
	return portfolioEventFixture(buyIntent, buyFill), portfolioEventFixture(sellIntent, sellFill)
}

func portfolioEventFixture(intent domain.IntentSnapshot, fill domain.CoreFill) *commonv1.ExecutionEvent {
	side := commonv1.OrderSide_ORDER_SIDE_BUY
	if intent.Input.Side == domain.SideSell {
		side = commonv1.OrderSide_ORDER_SIDE_SELL
	}
	return &commonv1.ExecutionEvent{
		EventId:          fmt.Sprintf("fill:simulator:%d", fill.ID),
		EventType:        domain.EventTradeFilled,
		SchemaVersion:    domain.EventSchemaVersion,
		OccurredAtMs:     time.Now().UnixMilli(),
		Producer:         domain.EventProducerExecution,
		AggregateId:      fill.ID,
		AggregateVersion: 1,
		Fill: &commonv1.FillEvent{
			TradeId:            fill.TradeID,
			ExecutionVenue:     domain.ExecutionVenueSimulator,
			CoreOrderId:        fill.CoreOrderID,
			IntentId:           intent.ID,
			AccountId:          intent.Input.AccountID,
			StrategyInstanceId: intent.Input.Authority.StrategyID,
			InstrumentId:       intent.Input.InstrumentID,
			Side:               side,
			Price:              fill.Price.String(),
			Quantity:           fill.Quantity.String(),
			QuoteNotional:      fill.QuoteNotional.String(),
			FeeAmount:          fill.FeeAmount.String(),
			FeeAsset:           fill.FeeAsset,
			SourceSequence:     fill.ID,
		},
	}
}

func portfolioNotice(ctx context.Context, t *testing.T, event *commonv1.ExecutionEvent) domain.FillNotice {
	t.Helper()
	notice, err := eventwire.Fill(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	return notice
}

func assertPortfolioRowCount(ctx context.Context, t *testing.T, db *gorm.DB, entity any, expected int64) {
	t.Helper()
	var count int64
	if err := db.WithContext(ctx).Model(entity).Count(&count).Error; err != nil || count != expected {
		t.Fatalf("%T count = %d, %v; expected %d", entity, count, err, expected)
	}
}
