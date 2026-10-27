//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestIntentReservationAndTypedOutboxCommitOrRollBackTogether(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, submit := admissionFixture(ctx, t, db)
	intent, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || intent.State != domain.OrderStateIntent || intent.ID <= 0 || intent.ClientOrderID == "" || !intent.ReservedQuoteNotional.Equal(decimal.NewFromInt(200)) {
		t.Fatalf("durable intent = %+v, %v", intent, err)
	}
	again, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || again.ID != intent.ID || again.ClientOrderID != intent.ClientOrderID {
		t.Fatalf("idempotent reservation = %+v, %v", again, err)
	}
	changed := submit.Input
	changed.Quantity = decimal.NewFromInt(3)
	if _, err := store.Accept(ctx, changed, submit.ExecutionAuthority); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("different business content reused an intent: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	var events []*modelv1.OutboxEvent
	if err := db.WithContext(ctx).Model(&modelv1.OutboxEvent{}).Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Event == nil || events[0].State != domain.OutboxStatePending || events[0].Event.Intent == nil || events[0].Event.Fill != nil {
		t.Fatalf("typed outbox event = %v", events)
	}
	payload := events[0].Event.Intent
	if payload.IntentId != intent.ID || payload.ClientOrderId != intent.ClientOrderID || payload.Price != "100" || payload.Quantity != "2" || payload.ReservedQuoteNotional != "200" || payload.State != commonv1.OrderState_ORDER_STATE_INTENT {
		t.Fatalf("outbox decimal/identity round trip = %v", payload)
	}
	if events[0].Event.EventId != events[0].EventId || events[0].Event.AggregateId != intent.ID || events[0].Event.AggregateVersion != 1 {
		t.Fatal("outbox envelope disagrees with its aggregate")
	}
	assertAdmissionRollback(ctx, t, db, store, submit)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
	assertGatewayOrderCount(ctx, t, db, 0) // Admission never calls a trading gateway.
}

func assertAdmissionRollback(ctx context.Context, t *testing.T, db *gorm.DB, store domain.IntentRepository, submit domain.GatewaySubmit) {
	t.Helper()
	injected := errors.New("controlled outbox write failure")
	const callbackName = "muskdeer:test:reject-outbox"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.OutboxEvent); ok {
			tx.Error = injected
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	submit.Input.IdempotencyKey = "rolled-back-reservation"
	submit.Input.Quantity = decimal.NewFromInt(1)
	if _, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, injected) {
		t.Fatalf("injected outbox failure lost its cause: %v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("failed outbox left an intent: %d, %v", count, err)
	}
}

func TestConcurrentStrategiesCannotOverReserveSharedAccount(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, first := admissionFixture(ctx, t, db)
	second := admissionSibling(ctx, t, db, first)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, submit := range []domain.GatewaySubmit{first, second} {
		workers.Go(func() {
			<-start
			_, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	winners, denied := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case errors.Is(err, domain.ErrRiskDenied):
			denied++
		default:
			t.Fatal(err)
		}
	}
	if winners != 1 || denied != 1 {
		t.Fatalf("concurrent strategies over-reserved: %d accepted, %d denied", winners, denied)
	}
	assertReservedBudget(ctx, t, db, first.Input.AccountID, "200")
}

func TestStaleOrWrongSourceRiskCannotReserveAnIntent(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, submit := admissionFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).
		UpdateColumn("market_data_source", string(domain.MarketSourceOKX)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("wrong source admitted risk: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).
		Select("MarketDataSource", "ObservedAt", "ExpiresAt").Updates(&modelv1.RiskSnapshot{
		MarketDataSource: string(domain.MarketSourceBinance),
		ObservedAt:       1,
		ExpiresAt:        2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("stale risk snapshot admitted risk: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func admissionFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.IntentRepository, domain.GatewaySubmit) {
	t.Helper()
	_, submit := coreFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(&modelv1.AccountBudget{
		AccountId:        submit.Input.AccountID,
		Asset:            "USDT",
		LimitNotional:    "300",
		ReservedNotional: "0",
		Enabled:          true,
		Revision:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	seedAdmissionStrategy(ctx, t, db, submit.Input)
	store, err := repository.NewIntents(db)
	if err != nil {
		t.Fatal(err)
	}
	return store, submit
}

func seedAdmissionStrategy(ctx context.Context, t *testing.T, db *gorm.DB, input domain.OrderInput) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
		StrategyInstanceId: input.Authority.StrategyID,
		AccountId:          input.AccountID,
		LimitNotional:      "300",
		ReservedNotional:   "0",
		Enabled:            true,
		Revision:           1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	feed, _, _, publication := admissionSelectedMarket(ctx, t, db, input.InstrumentID)
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", input.Authority.StrategyID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Create(&modelv1.RiskSnapshot{
		StrategyInstanceId:  input.Authority.StrategyID,
		AccountId:           input.AccountID,
		InstrumentId:        input.InstrumentID,
		ConfigVersion:       input.Authority.ConfigVersion,
		MarketDataSource:    string(domain.MarketSourceBinance),
		MarketEpoch:         publication.OwnerEpoch,
		MarketSequence:      publication.Reading.Sequence,
		MarketFeedId:        &feed.ID,
		MarketRevision:      publication.Revision,
		MarketSourceVersion: feed.SourceVersion,
		MarketMaximumAgeMs:  5000,
		ObservedAt:          clock.NowMS,
		ExpiresAt:           clock.NowMS + domain.MaxRiskSnapshotAge.Milliseconds(),
		Reconciled:          true,
		AllowNewRisk:        true,
		Revision:            1,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func admissionSibling(ctx context.Context, t *testing.T, db *gorm.DB, original domain.GatewaySubmit) domain.GatewaySubmit {
	t.Helper()
	strategy := identityInstance(original.Input.InstrumentID, "shared-budget-strategy", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	strategy.AccountId = original.Input.AccountID
	strategy.State = string(domain.StrategyStateRunning)
	strategy.DesiredRunning = true
	strategy.ActiveConfigVersion = proto.Int64(1)
	strategy.ActiveMarketDataSource = proto.String(string(domain.MarketSourceBinance))
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(strategy).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = strategy.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	lease := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: strategy.Id,
	})
	original.Input.Authority = domain.StrategyAuthority{
		StrategyID:    strategy.Id,
		ConfigVersion: 1,
		OwnerID:       lease.OwnerID,
		OwnerEpoch:    lease.Epoch,
	}
	original.Input.IdempotencyKey = "second-strategy-intent"
	seedAdmissionStrategy(ctx, t, db, original.Input)
	return original
}

func assertReservedBudget(ctx context.Context, t *testing.T, db *gorm.DB, accountID, expected string) {
	t.Helper()
	var budget modelv1.AccountBudget
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", accountID).First(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(budget.ReservedNotional).Equal(decimal.RequireFromString(expected)) {
		t.Fatalf("shared reserved notional = %s, expected %s", budget.ReservedNotional, expected)
	}
}
