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

	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	portfoliomodelv1 "github.com/hmlax/muskdeer/deer/pb/portfolio/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminFlatteningAcceptancePersistsExactScopeAndReplaysHistoricalConfirmation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, submit, request := flattenAcceptanceFixture(ctx, t, db)
	store := flattenAcceptanceStore(t, db)
	before := publicationInstance(ctx, t, db, request.StrategyID)
	receipt, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if err != nil || receipt.ID <= 0 || receipt.ActorUserID != admin.Principal.UserID || receipt.StrategyID != request.StrategyID || receipt.AccountID != request.AccountID ||
		receipt.InstrumentID != request.InstrumentID || receipt.CancellationID != request.CancellationID || !receipt.ConfirmedQuantity.Equal(decimal.NewFromInt(10)) || !receipt.LimitPrice.Equal(request.LimitPrice) {
		t.Fatal("actual flatten request did not persist exact confirmation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 1)
	assertFlatAuditCount(ctx, t, db, 1)
	if !proto.Equal(before, publicationInstance(ctx, t, db, request.StrategyID)) {
		t.Fatal("metadata acceptance changed strategy state or target")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	// Explicit post-acceptance balance fault: replay must retain history and a
	// viewer read must never repair the projection or manufacture funds.
	if err := db.WithContext(ctx).Model(&executionmodelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", request.AccountID, "BTC").
		Select("Available", "Revision").Updates(&executionmodelv1.SimulatorBalance{
		Available: "9",
		Revision:  request.ExpectedBaseRevision + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	replayed, err := store.Request(ctx, admin.Token, admin.CSRF, request)
	if err != nil || replayed.ID != receipt.ID || !replayed.ConfirmedQuantity.Equal(receipt.ConfirmedQuantity) || replayed.ExpectedBaseRevision != receipt.ExpectedBaseRevision {
		t.Fatal("same-key replay recomputed old confirmation from later funds", err)
	}
	viewer := riskActorFixture(ctx, t, db, "flatten-acceptance-viewer", domain.AdminRoleViewer, 246, domain.AdminSessionTTL)
	observed, err := store.Confirmation(ctx, viewer.Token, request.StrategyID, receipt.ID)
	if err != nil || observed.ID != receipt.ID || observed.ExpectedRiskRevision != request.ExpectedRiskRevision || !observed.ConfirmedQuantity.Equal(request.ConfirmedQuantity) {
		t.Fatal("current viewer lost the historical scoped confirmation", err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "9", "0")
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 1)
	assertFlatAuditCount(ctx, t, db, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminFlatteningAcceptanceConcurrentOriginalKeyHasOneEffectAndBlocksNewKeys(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, _, request := flattenAcceptanceFixture(ctx, t, db)
	store := flattenAcceptanceStore(t, db)
	start := make(chan struct{})
	results := make(chan domain.AdminFlattenReceipt, 4)
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			result, err := store.Request(ctx, admin.Token, admin.CSRF, request)
			results <- result
			failures <- err
		})
	}
	close(start)
	workers.Wait()
	var id int64
	for range 4 {
		result, err := <-results, <-failures
		if err != nil || result.ID <= 0 || id != 0 && id != result.ID {
			t.Fatal("concurrent original key did not return one confirmation", err)
		}
		id = result.ID
	}
	changed := request
	changed.Reason += " changed"
	if _, err := store.Request(ctx, admin.Token, admin.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("same key with changed flatten content was accepted", err)
	}
	changed = request
	changed.Key = "flatten-second-pending-key-0001"
	if _, err := store.Request(ctx, admin.Token, admin.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("new key bypassed the account's pending confirmation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.AdminAccountFlattening{}, 1)
	assertFlatAuditCount(ctx, t, db, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func flattenAcceptanceFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, domain.GatewaySubmit, domain.AdminFlattenRequest) {
	t.Helper()
	operator, _, submit := adminCreationFixture(ctx, t, db)
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, operator.Token, operator.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	if work, err := emergencyProgressStore(t, db).Prepare(ctx, accepted.StrategyID, 0); err != nil || work.State != domain.AdminEmergencyCancelSucceeded {
		t.Fatal("flatten request fixture lacks completed real cancellation proof", err)
	}
	admin := riskActorFixture(ctx, t, db, "flatten-acceptance-admin", domain.AdminRoleAdmin, 250, domain.AdminSessionTTL)
	var portfolio portfoliomodelv1.PortfolioAccount
	if err := db.WithContext(ctx).Model(&portfoliomodelv1.PortfolioAccount{}).Where("account_id = ?", submit.Input.AccountID).Take(&portfolio).Error; err != nil {
		t.Fatal(err)
	}
	var balance executionmodelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&executionmodelv1.SimulatorBalance{}).Where("account_id = ? AND asset = ?", submit.Input.AccountID, portfolio.BaseAsset).Take(&balance).Error; err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseLedgerDecimal(portfolio.Quantity, false, true)
	if err != nil {
		t.Fatal(err)
	}
	return admin, submit, domain.AdminFlattenRequest{
		Key:                       "flatten-acceptance-original-key",
		Scope:                     domain.AdminFlattenScopeAccountInstrument,
		StrategyID:                accepted.StrategyID,
		AccountID:                 submit.Input.AccountID,
		InstrumentID:              submit.Input.InstrumentID,
		CancellationID:            accepted.ID,
		ExpectedRiskRevision:      accepted.RiskRevision,
		ExpectedPortfolioRevision: portfolio.Revision,
		ExpectedBaseRevision:      balance.Revision,
		ConfirmedQuantity:         quantity,
		LimitPrice:                decimal.RequireFromString("10.01"),
		Reason:                    "administrator confirms isolated simulated account inventory",
	}
}

func flattenAcceptanceStore(t *testing.T, db *gorm.DB) domain.AdminAccountFlattenings {
	t.Helper()
	store, err := repository.NewAdminAccountFlattenings(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func assertFlatAuditCount(ctx context.Context, t *testing.T, db *gorm.DB, expected int64) {
	t.Helper()
	var count int64
	if err := db.WithContext(ctx).Model(&commonmodelv1.AdminAudit{}).Where("action = ?", domain.AdminFlattenAction).Count(&count).Error; err != nil || count != expected {
		t.Fatal("flatten request left the wrong number of separate audits", err, count)
	}
}
