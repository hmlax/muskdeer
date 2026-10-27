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

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestCoreFreezesSettlesPartialFillsAndConfirmsCancellation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	order, err := core.Submit(ctx, submit)
	if err != nil || order.State != domain.OrderStateLive || !order.FrozenAmount.Equal(decimal.RequireFromString("200.2")) {
		t.Fatalf("core submit = %+v, %v", order, err)
	}
	again, err := core.Submit(ctx, submit)
	if err != nil || again.ID != order.ID {
		t.Fatalf("idempotent core submit = %+v, %v", again, err)
	}
	changed := submit
	changed.Input.Quantity = decimal.NewFromInt(3)
	if _, err := core.Submit(ctx, changed); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed business content reused identity: %v", err)
	}
	fill := domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       submit.ClientOrderID,
		TradeID:             "external-trade-1",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(90),
		Quantity:            decimal.RequireFromString("0.5"),
	}
	execution, err := core.Fill(ctx, fill)
	if err != nil || !execution.FeeAmount.Equal(decimal.RequireFromString("0.045")) {
		t.Fatalf("partial execution = %+v, %v", execution, err)
	}
	duplicate, err := core.Fill(ctx, fill)
	if err != nil || duplicate.ID != execution.ID {
		t.Fatalf("duplicate execution changed funds: %+v, %v", duplicate, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "804.805", "150.15")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10.5", "0")
	cancellation := domain.GatewayCancel{
		Input: domain.CancelInput{
			IdempotencyKey: "cancel-core-1",
			Authority:      submit.Input.Authority,
			AccountID:      submit.Input.AccountID,
			InstrumentID:   submit.Input.InstrumentID,
			ClientOrderID:  submit.ClientOrderID,
			Deadline:       submit.Input.Deadline,
		},
		ExecutionAuthority: submit.ExecutionAuthority,
	}
	pending, err := core.Cancel(ctx, cancellation)
	if err != nil || pending.CancelState != domain.CancelStatePending || pending.State != domain.OrderStatePartiallyFilled {
		t.Fatalf("accepted cancellation erased fill/state: %+v, %v", pending, err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "804.805", "150.15")
	fill.TradeID = "external-trade-2"
	fill.Price = decimal.NewFromInt(100)
	if _, err := core.Fill(ctx, fill); err != nil {
		t.Fatal(err)
	}
	closed, err := core.ConfirmCancel(ctx, submit.Input.AccountID, submit.ClientOrderID)
	if err != nil || closed.State != domain.OrderStateCanceled || !closed.FilledQuantity.Equal(decimal.NewFromInt(1)) || !closed.FrozenAmount.IsZero() {
		t.Fatalf("confirmed cancel = %+v, %v", closed, err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, submit.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "904.905", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "11", "0")
	if _, err := core.Fill(ctx, fill); err != nil { // A known fact stays queryable after termination.
		t.Fatal(err)
	}
	fill.TradeID = "late-new-trade"
	if _, err := core.Fill(ctx, fill); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("new execution resurrected canceled order: %v", err)
	}
}

func TestCoreSerializesConcurrentFundsAndRejectsSelfTrades(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for index := range 2 {
		candidate := submit
		candidate.Input.Quantity = decimal.NewFromInt(6)
		if index == 1 {
			candidate.ClientOrderID = "contender-2"
			candidate.Input.IdempotencyKey = "submit-contender-2"
		}
		workers.Go(func() {
			<-start
			_, err := core.Submit(ctx, candidate)
			results <- err
		})
	}
	close(start)
	workers.Wait()
	winners, rejected := 0, 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			winners++
		case errors.Is(err, domain.ErrInsufficientFunds):
			rejected++
		default:
			t.Fatal(err)
		}
	}
	if winners != 1 || rejected != 1 {
		t.Fatalf("concurrent account spend: %d winners, %d rejected", winners, rejected)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "399.4", "600.6")
	sell := submit
	sell.ClientOrderID = "self-crossing-sell"
	sell.Input.IdempotencyKey = "self-crossing-intent"
	sell.Input.Side = domain.SideSell
	sell.Input.Price = decimal.NewFromInt(99)
	if _, err := core.Submit(ctx, sell); !errors.Is(err, domain.ErrSelfTrade) {
		t.Fatalf("self crossing was accepted: %v", err)
	}
	sibling := coreSibling(ctx, t, db, sell)
	if _, err := core.Submit(ctx, sibling); !errors.Is(err, domain.ErrSelfTrade) {
		t.Fatalf("same-subject crossing across accounts was accepted: %v", err)
	}
}

func coreSibling(ctx context.Context, t *testing.T, db *gorm.DB, original domain.GatewaySubmit) domain.GatewaySubmit {
	t.Helper()
	const accountID = "sibling-self-account"
	strategy := identityInstance(original.Input.InstrumentID, "sibling-strategy", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	strategy.AccountId = accountID
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
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(&modelv1.SimulatorAccount{
		AccountId:    accountID,
		SubjectId:    "fixture-subject",
		Enabled:      true,
		QuoteFeeRate: "0.001",
		Revision:     1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	strategyLease := acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: strategy.Id,
	})
	original.ExecutionAuthority = acquireGatewayLease(ctx, t, db, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: accountID,
	})
	original.Input.AccountID = accountID
	original.Input.Authority = domain.StrategyAuthority{
		StrategyID:    strategy.Id,
		ConfigVersion: 1,
		OwnerID:       strategyLease.OwnerID,
		OwnerEpoch:    strategyLease.Epoch,
	}
	return original
}

func TestCoreSellFeesAndChangedTradeIdentity(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	submit.Input.Side = domain.SideSell
	submit.Input.ReduceOnly = true
	if _, err := core.Submit(ctx, submit); err != nil {
		t.Fatal(err)
	}
	fill := domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       submit.ClientOrderID,
		TradeID:             "sell-fill-1",
		CounterpartySubject: "fixture-subject",
		Price:               decimal.NewFromInt(101),
		Quantity:            decimal.NewFromInt(2),
	}
	if _, err := core.Fill(ctx, fill); !errors.Is(err, domain.ErrSelfTrade) {
		t.Fatalf("same subject supplied execution: %v", err)
	}
	fill.CounterpartySubject = "external-liquidity"
	if _, err := core.Fill(ctx, fill); err != nil {
		t.Fatal(err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1201.798", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "8", "0")
	fill.Price = decimal.NewFromInt(102)
	if _, err := core.Fill(ctx, fill); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("changed trade reused natural identity: %v", err)
	}
}

func TestCoreFinalFenceRollsBackAlreadyStagedFunds(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	const callbackName = "muskdeer:test:expire-staged-order"
	if err := db.Callback().Create().After("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.SimulatorOrder); !ok {
			return
		}
		// The callback shares this exact SQL transaction. It schedules expiry
		// after funds and the order have been written, before outer commit.
		err := tx.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.StrategyLease{}).
			Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).UpdateColumn("expires_at", 0).Error
		tx.Error = errors.Join(tx.Error, err)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callbackName); err != nil {
			t.Error(err)
		}
	})
	if _, err := core.Submit(ctx, submit); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("final expiry accepted staged core effects: %v", err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func coreFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.Simulator, domain.GatewaySubmit) {
	t.Helper()
	command := gatewayCommandFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ?", command.InstrumentId).UpdateColumn("min_notional", "1").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(&modelv1.SimulatorAccount{
		AccountId:    command.AccountId,
		SubjectId:    "fixture-subject",
		Enabled:      true,
		QuoteFeeRate: "0.001",
		Revision:     1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []*modelv1.SimulatorBalance{
		{
			AccountId: command.AccountId,
			Asset:     "BTC",
			Available: "10",
			Frozen:    "0",
			Revision:  1,
		},
		{
			AccountId: command.AccountId,
			Asset:     "USDT",
			Available: "1000",
			Frozen:    "0",
			Revision:  1,
		},
	} {
		if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	core, err := repository.NewSimulator(db)
	if err != nil {
		t.Fatal(err)
	}
	deadline, _ := ctx.Deadline() // The request cannot outlive its native caller.
	return core, domain.GatewaySubmit{
		Input: domain.OrderInput{
			IdempotencyKey: "core-submit-1",
			Authority: domain.StrategyAuthority{
				StrategyID:    command.StrategyInstanceId,
				ConfigVersion: 1,
				OwnerID:       command.OwnerId,
				OwnerEpoch:    command.OwnerEpoch,
			},
			AccountID:    command.AccountId,
			InstrumentID: command.InstrumentId,
			Side:         domain.SideBuy,
			Price:        decimal.NewFromInt(100),
			Quantity:     decimal.NewFromInt(2),
			TimeInForce:  domain.TimeInForcePostOnly,
			Deadline:     deadline,
		},
		ClientOrderID: "core-client-1",
		ExecutionAuthority: domain.LeaseToken{
			Key: domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: command.AccountId,
			},
			OwnerID: command.ExecutionOwnerId,
			Epoch:   command.ExecutionEpoch,
		},
	}
}

func assertCoreBalance(ctx context.Context, t *testing.T, core domain.Simulator, account, asset, available, frozen string) {
	t.Helper()
	balances, err := core.Balances(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	for _, balance := range balances {
		if balance.Asset == asset {
			if !balance.Available.Equal(decimal.RequireFromString(available)) || !balance.Frozen.Equal(decimal.RequireFromString(frozen)) {
				t.Fatalf("%s funds = %s/%s, expected %s/%s", asset, balance.Available, balance.Frozen, available, frozen)
			}
			return
		}
	}
	t.Fatalf("asset %s missing", asset)
}
