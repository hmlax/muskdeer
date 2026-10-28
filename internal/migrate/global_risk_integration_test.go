//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestExplicitPerOrderGlobalLimitRejectsWithoutAnyReservation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, submit := admissionFixture(ctx, t, db)
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopePlatform)).UpdateColumn("maximum_order_notional", "150").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("per-order risk limit ignored: %v", err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	var budgets []*modelv1.RiskBudget
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Find(&budgets).Error; err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 2 {
		t.Fatal("explicit platform/instrument limits missing")
	}
	for _, budget := range budgets {
		value, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil || !value.IsZero() {
			t.Fatalf("failed acceptance changed a global reservation: %v, %v", budget, err)
		}
	}
}

func TestDuplicateIntentDoesNotDoubleGlobalReservation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, submit := admissionFixture(ctx, t, db)
	first, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || first.ID != again.ID {
		t.Fatalf("duplicate intent changed identity: %+v, %v", again, err)
	}
	var budgets []*modelv1.RiskBudget
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Find(&budgets).Error; err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 2 {
		t.Fatal("explicit global limits missing")
	}
	for _, budget := range budgets {
		value, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
		if err != nil || !value.Equal(decimal.NewFromInt(200)) {
			t.Fatalf("duplicate intent changed global reserve: %v, %v", budget, err)
		}
	}
}

func TestFinalGlobalDisableRejectsPreparedOrderAndReleasesAllScopes(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, store := executionFixture(t, db)
	observed := &observedGateway{
		TradingGateway: core,
		before: func(ctx context.Context, _ domain.GatewaySubmit) error {
			return db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopePlatform)).UpdateColumn("enabled", false).Error
		},
	}
	service := intentServiceFixture(t, store, observed)
	rejected, err := service.Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || rejected.State != domain.OrderStateRejected || rejected.ErrorCode != domain.IntentErrorRiskDenied {
		t.Fatalf("disabled final global budget accepted order: %+v, %v", rejected, err)
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func seedGlobalRiskBudgets(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64) {
	t.Helper()
	for _, row := range []*modelv1.RiskBudget{
		{
			ScopeKind:            string(domain.RiskScopePlatform),
			ScopeKey:             "USDT",
			Asset:                "USDT",
			LimitNotional:        "10000",
			ReservedNotional:     "0",
			MaximumOrderNotional: "300",
			Enabled:              true,
			Revision:             1,
		},
		{
			ScopeKind:            string(domain.RiskScopeInstrument),
			ScopeKey:             strconv.FormatInt(instrumentID, 10),
			InstrumentId:         &instrumentID,
			Asset:                "USDT",
			LimitNotional:        "10000",
			ReservedNotional:     "0",
			MaximumOrderNotional: "300",
			Enabled:              true,
			Revision:             1,
		},
	} {
		if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestCrossAccountOrdersCannotOverReserveInstrumentOrPlatform(t *testing.T) {
	for _, scope := range []domain.RiskScope{domain.RiskScopePlatform, domain.RiskScopeInstrument} {
		t.Run(string(scope), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			store, first := admissionFixture(ctx, t, db)
			peer := first
			peer.Input.AccountID = "global-other-account"
			seedGlobalPeerAssets(ctx, t, db, peer.Input.AccountID)
			if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(&modelv1.AccountBudget{
				AccountId:        peer.Input.AccountID,
				Asset:            "USDT",
				LimitNotional:    "300",
				ReservedNotional: "0",
				Enabled:          true,
				Revision:         1,
			}).Error; err != nil {
				t.Fatal(err)
			}
			peer.ExecutionAuthority = acquireGatewayLease(ctx, t, db, domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: peer.Input.AccountID,
			})
			second := admissionSibling(ctx, t, db, peer)
			if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(scope)).UpdateColumn("limit_notional", "300").Error; err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for _, input := range []domain.GatewaySubmit{first, second} {
				workers.Go(func() {
					<-start
					_, err := store.Accept(ctx, input.Input, input.ExecutionAuthority)
					results <- err
				})
			}
			close(start)
			workers.Wait()
			close(results)
			winners, denied := 0, 0
			for err := range results {
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
				t.Fatalf("%s did not serialize cross-account risk: accepted=%d denied=%d", scope, winners, denied)
			}
			var row modelv1.RiskBudget
			if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(scope)).First(&row).Error; err != nil {
				t.Fatal(err)
			}
			value, err := domain.ParseLedgerDecimal(row.ReservedNotional, false, true)
			if err != nil || !value.Equal(decimal.NewFromInt(200)) {
				t.Fatalf("%s reservation changed: %v, %v", scope, &row, err)
			}
		})
	}
}

func seedGlobalPeerAssets(ctx context.Context, t *testing.T, db *gorm.DB, accountID string) {
	t.Helper()
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(&modelv1.SimulatorAccount{
		AccountId:    accountID,
		SubjectId:    "global-budget-peer-subject",
		Enabled:      true,
		QuoteFeeRate: "0.001",
		Revision:     1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	for _, balance := range []*modelv1.SimulatorBalance{
		{
			AccountId: accountID,
			Asset:     "BTC",
			Available: "10",
			Frozen:    "0",
			Revision:  1,
		},
		{
			AccountId: accountID,
			Asset:     "USDT",
			Available: "1000",
			Frozen:    "0",
			Revision:  1,
		},
	} {
		if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Create(balance).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestGlobalCountersAndIntentIdentityCannotBeRewritten(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, submit := admissionFixture(ctx, t, db)
	accepted, err := store.Accept(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	err = db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopePlatform)).UpdateColumn("reserved_notional", "0").Error
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ?", string(domain.RiskScopePlatform)).UpdateColumn("asset", "USDC").Error
	assertDatabaseCode(t, err, "23514")
	err = db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", accepted.ID).UpdateColumn("price", "99").Error
	assertDatabaseCode(t, err, "23514")
}

func TestGlobalBudgetUpgradeCarriesUnknownObligationsWithoutEnablingRisk(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:14]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	instrument.MinNotional = "1"
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	strategy := identityInstance(instrument.Id, "legacy-global-risk", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Omit("RuntimeOwnerId", "RuntimeOwnerEpoch", "DesiredState").Create(strategy).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = strategy.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(&modelv1.AccountBudget{
		AccountId:        strategy.AccountId,
		Asset:            "USDT",
		LimitNotional:    "300",
		ReservedNotional: "200",
		Enabled:          true,
		Revision:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
		StrategyInstanceId: strategy.Id,
		AccountId:          strategy.AccountId,
		LimitNotional:      "300",
		ReservedNotional:   "200",
		Enabled:            true,
		Revision:           1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	intent := &modelv1.OrderIntent{
		AccountId:             strategy.AccountId,
		IdempotencyKey:        "legacy-unknown-global",
		ClientOrderId:         "legacy-unknown-client",
		StrategyInstanceId:    strategy.Id,
		ConfigVersion:         1,
		InstrumentId:          instrument.Id,
		RequestId:             "legacy-request",
		Fingerprint:           strings.Repeat("0", 64),
		Side:                  string(domain.SideBuy),
		Price:                 "100",
		Quantity:              "2",
		FilledQuantity:        "0",
		ReservedQuoteNotional: "200",
		TimeInForce:           string(domain.TimeInForcePostOnly),
		State:                 string(domain.OrderStateUnknown),
		CancelState:           string(domain.CancelStateNone),
		OwnerId:               "legacy-owner",
		OwnerEpoch:            1,
		ExecutionOwnerId:      "legacy-execution",
		ExecutionEpoch:        1,
		SubmissionDeadlineAt:  1,
		Revision:              1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Create(intent).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var budgets []*modelv1.RiskBudget
	if err := db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Find(&budgets).Error; err != nil {
		t.Fatal(err)
	}
	if len(budgets) != 2 {
		t.Fatal("legacy obligations were not carried to both scopes")
	}
	for _, row := range budgets {
		reserved, err := domain.ParseLedgerDecimal(row.ReservedNotional, false, true)
		if err != nil || row.Enabled || !reserved.Equal(decimal.NewFromInt(200)) || row.Id <= 0 || row.CreateAt <= 0 || row.UpdateAt <= 0 || row.DeleteAt != nil {
			t.Fatalf("upgrade lost or enabled legacy exposure: %v, %v", row, err)
		}
	}
}
