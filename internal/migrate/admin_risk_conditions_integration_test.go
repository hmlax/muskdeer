//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskRecoveryDoesNotOverrideIndependentSharedGates(t *testing.T) {
	for _, scope := range []string{"account", "instrument", "platform", "reconciliation"} {
		t.Run(scope, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
			control := riskControlFixture(t, db)
			instance := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
			if _, err := control.Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(instance.Id)); err != nil {
				t.Fatal(err)
			}
			closeRiskIndependentGate(ctx, t, db, scope, instance)
			refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
			if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(instance)); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
				t.Fatal("recovery bypassed an independent shared gate", err)
			}
			assertRiskControlState(ctx, t, control, actor, instance.Id, false, 2)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 1)
			assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
			assertGatewayOrderCount(ctx, t, db, 0)
		})
	}
}

func closeRiskIndependentGate(ctx context.Context, t *testing.T, db *gorm.DB, scope string, instance *modelv1.StrategyInstance) {
	t.Helper()
	var result *gorm.DB
	switch scope {
	case "account":
		result = db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", instance.AccountId).UpdateColumn("enabled", false)
	case "instrument":
		result = db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ? AND instrument_id = ?", string(domain.RiskScopeInstrument), instance.InstrumentId).UpdateColumn("enabled", false)
	case "platform":
		result = db.WithContext(ctx).Model(&modelv1.RiskBudget{}).Where("scope_kind = ? AND asset = ?", string(domain.RiskScopePlatform), "USDT").UpdateColumn("enabled", false)
	case "reconciliation":
		result = db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", instance.AccountId).UpdateColumn("reconciled", false)
	default:
		t.Fatal("unknown risk fixture scope")
	}
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("independent fixture gate was not closed", result.Error)
	}
}

func TestAdminRiskRecoveryBindsCurrentRequestedConfigurationAndInventory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	if _, err := control.Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(before.Id)); err != nil {
		t.Fatal(err)
	}
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	request.Source = domain.MarketSourceBinance
	request.Parameters.Maker.InventoryHardLimit = decimal.NewFromInt(1)
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); err != nil {
		t.Fatal("could not publish a new requested inventory policy", err)
	}
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(before)); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("old confirmation silently accepted a changed configuration", err)
	}
	current := publicationInstance(ctx, t, db, before.Id)
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(current)); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("recovery ignored the requested cap below actual inventory", err)
	}
	assertRiskControlState(ctx, t, control, actor, before.Id, false, 2)
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminRiskRecoveryRequiresFreshSelectedSourceAndCurrentTarget(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "risk-administrator", domain.AdminRoleAdmin, 61, domain.AdminSessionTTL)
	control := riskControlFixture(t, db)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	if _, err := control.Change(ctx, actor.Token, actor.CSRF, riskBlockRequest(before.Id)); err != nil {
		t.Fatal(err)
	}
	feed, owner, snapshots, current := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	invalid := marketReadingFixture(feed, current.Reading.Sequence+1, "100")
	invalid.Quality, invalid.Reason = domain.MarketQualityInvalid, domain.MarketReasonSequence
	invalid.Bid, invalid.Ask, invalid.Mid = decimal.Zero, decimal.Zero, decimal.Zero
	invalid.BidQuantity, invalid.AskQuantity = decimal.Zero, decimal.Zero
	if _, err := snapshots.Publish(ctx, owner, current.Revision, invalid); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(before)); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("invalid selected source permitted recovery", err)
	}
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "risk-confirmation-target-change-0001",
		StrategyID:       before.Id,
		ExpectedRevision: before.Revision,
		Action:           domain.AdminControlPause,
		Reason:           "change target after the old recovery confirmation",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(before)); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("old confirmation silently accepted a changed runtime target", err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	currentInstance := publicationInstance(ctx, t, db, before.Id)
	if _, err := control.Change(ctx, admin.Token, admin.CSRF, riskRestoreRequest(currentInstance)); err != nil {
		t.Fatal("current paused target could not recover after fresh selected data", err)
	}
	assertControlInstance(ctx, t, db, before.Id, domain.StrategyStateRunning, domain.StrategyStatePaused, currentInstance.Revision)
	assertGatewayOrderCount(ctx, t, db, 0)
}
