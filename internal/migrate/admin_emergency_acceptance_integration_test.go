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

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminEmergencyRequestAtomicallyBlocksRiskWithoutReleasingLiveUnsentOrUnknownObligations(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	live, unsent, unknown := adminOrderFacts(ctx, t, db, core, intents, submit)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	budget := activationBudget(ctx, t, db, before.Id)
	sibling := publicationInstance(ctx, t, db, created.StrategyID)
	siblingBudget := activationBudget(ctx, t, db, sibling.Id)
	var balances []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ?", submit.Input.AccountID).Order("id ASC").Find(&balances).Error; err != nil {
		t.Fatal(err)
	}
	store := emergencyAcceptanceStore(t, db)
	request := emergencyAcceptanceRequest(before.Id)
	first, err := store.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil || first.ID <= 0 || first.StrategyID != before.Id || first.Scope != domain.AdminEmergencyCancelScopeStrategy || first.ExpectedRiskRevision != 1 || first.RiskRevision != 2 {
		t.Fatal("scoped emergency acceptance did not close independent risk", err)
	}
	again, err := store.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil || again != first {
		t.Fatal("original request did not return immutable acceptance", err)
	}
	changed := request
	changed.Reason = "different scope confirmation"
	if _, err := store.Request(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("same key changed emergency confirmation", err)
	}
	stale := request
	stale.Key = "different-emergency-request-0001"
	if _, err := store.Request(ctx, actor.Token, actor.CSRF, stale); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("new scope request ignored a stale independent risk revision", err)
	}
	assertRiskControlState(ctx, t, riskControlFixture(t, db), actor, before.Id, false, 2)
	currentBudget := activationBudget(ctx, t, db, before.Id)
	if currentBudget.ReservedNotional != budget.ReservedNotional || currentBudget.LimitNotional != budget.LimitNotional ||
		!proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(sibling, publicationInstance(ctx, t, db, sibling.Id)) ||
		!proto.Equal(siblingBudget, activationBudget(ctx, t, db, sibling.Id)) {
		t.Fatal("scope acceptance changed reservations, runtime or sibling strategy")
	}
	assertEmergencyUnchangedBalances(ctx, t, db, submit.Input.AccountID, balances)
	for _, original := range []domain.IntentSnapshot{live, unsent, unknown} {
		current, err := intents.ByID(ctx, submit.Input.AccountID, original.ID)
		if err != nil || current.State != original.State || !current.ReservedQuoteNotional.Equal(original.ReservedQuoteNotional) {
			t.Fatal("acceptance resolved an uncertain order or released its reservation", err)
		}
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertGatewayOrderCount(ctx, t, db, 2)
}

func TestAdminEmergencyAcceptanceReadsRequireCurrentViewerAndReplayRequiresOperatorCSRF(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "emergency-acceptance-viewer", domain.AdminRoleViewer, 151, domain.AdminSessionTTL)
	store := emergencyAcceptanceStore(t, db)
	request := emergencyAcceptanceRequest(submit.Input.Authority.StrategyID)
	receipt, err := store.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := store.Acceptance(ctx, viewer.Token, request.StrategyID, receipt.ID)
	if err != nil || observed != receipt {
		t.Fatal("current viewer lost immutable scope acceptance", err)
	}
	if result, err := store.Acceptance(ctx, viewer.Token, request.StrategyID+1, receipt.ID); !errors.Is(err, domain.ErrAdminNotFound) || result.ID != 0 {
		t.Fatal("receipt crossed its declared strategy", err)
	}
	if _, err := store.Request(ctx, viewer.Token, viewer.CSRF, request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("viewer wrote or replayed emergency scope", err)
	}
	if _, err := store.Request(ctx, actor.Token, viewer.CSRF, request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("historical replay bypassed independent CSRF proof", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, viewer.Token, viewer.CSRF); err != nil {
		t.Fatal(err)
	}
	if result, err := store.Acceptance(ctx, viewer.Token, request.StrategyID, receipt.ID); !errors.Is(err, domain.ErrUnauthenticated) || result.ID != 0 {
		t.Fatal("revoked viewer retained historical acceptance", err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if result, err := store.Request(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrUnauthenticated) || result.ID != 0 {
		t.Fatal("revoked actor replayed scoped work", err)
	}
}

func TestAdminEmergencyPendingScopeDeniesOtherwiseClearRiskRecovery(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	admin := riskActorFixture(ctx, t, db, "emergency-recovery-admin", domain.AdminRoleAdmin, 161, domain.AdminSessionTTL)
	instance := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	if _, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(instance.Id)); err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	restore := riskRestoreRequest(instance)
	if _, err := riskControlFixture(t, db).Change(ctx, admin.Token, admin.CSRF, restore); !errors.Is(err, domain.ErrAdminRiskRecoveryDenied) {
		t.Fatal("empty orders reopened risk before scoped work could be resolved", err)
	}
	assertRiskControlState(ctx, t, riskControlFixture(t, db), admin, instance.Id, false, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertEmergencyPendingDatabaseRecovery(ctx, t, db, admin, instance)
}

func assertEmergencyPendingDatabaseRecovery(ctx context.Context, t *testing.T, db *gorm.DB, admin domain.AdminLogin, instance *modelv1.StrategyInstance) {
	t.Helper()
	now := controlTestDatabaseTime(ctx, t, db, admin.Principal.UserID)
	// Explicitly bypass the repository to exercise the final COMMIT constraint.
	err := db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Transaction(func(tx *gorm.DB) error {
		audit := &modelv1.AdminAudit{
			Actor:         admin.Principal.Username,
			ActorRole:     string(admin.Principal.Role),
			Action:        string(domain.AdminRiskUnblock),
			Resource:      domain.AdminAuditResourceRisk + ":" + strconv.FormatInt(instance.Id, 10),
			Reason:        "test-only pending-scope recovery bypass",
			BeforeVersion: proto.Int64(2),
			AfterVersion:  proto.Int64(3),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(audit).Error; err != nil {
			return err
		}
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", instance.Id).UpdateColumn("enabled", true).Error; err != nil {
			return err
		}
		return tx.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Create(&modelv1.AdminStrategyRiskChange{
			ActorUserId:           admin.Principal.UserID,
			ActorSessionId:        admin.Principal.SessionID,
			StrategyInstanceId:    instance.Id,
			AuditId:               audit.Id,
			IdempotencyKey:        "emergency-recovery-bypass-0001",
			RequestFingerprint:    strings.Repeat("b", 64),
			Action:                string(domain.AdminRiskUnblock),
			PreviousEnabled:       false,
			Enabled:               true,
			PreviousRevision:      2,
			Revision:              3,
			Reason:                audit.Reason,
			RecoveryUntil:         proto.Int64(now + 4000),
			ExpectedConfigVersion: proto.Int64(instance.RequestedConfigVersion),
			ExpectedDesiredState:  proto.String(instance.DesiredState),
		}).Error
	})
	assertEmergencySchemaConstraint(t, err, "admin_risk_recovery")
	assertRiskControlState(ctx, t, riskControlFixture(t, db), admin, instance.Id, false, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
}

func TestAdminEmergencyConcurrentSameKeyHasOneClosureAndHistoricalAcceptance(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	store := emergencyAcceptanceStore(t, db)
	request := emergencyAcceptanceRequest(submit.Input.Authority.StrategyID)
	start := make(chan struct{})
	results := make(chan domain.AdminEmergencyCancelAcceptance, 4)
	failures := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			receipt, err := store.Request(ctx, actor.Token, actor.CSRF, request)
			results <- receipt
			failures <- err
		})
	}
	close(start)
	workers.Wait()
	var first domain.AdminEmergencyCancelAcceptance
	for range 4 {
		result, err := <-results, <-failures
		if err != nil || result.ID <= 0 || result.RiskRevision != 2 || first.ID != 0 && first != result {
			t.Fatal("concurrent duplicate had an extra effect or lost the original receipt", err)
		}
		first = result
	}
	assertRiskControlState(ctx, t, riskControlFixture(t, db), actor, request.StrategyID, false, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	if _, err := riskControlFixture(t, db).Change(ctx, actor.Token, actor.CSRF, domain.AdminRiskRequest{
		Key:              "later-independent-risk-block-0001",
		StrategyID:       request.StrategyID,
		ExpectedRevision: 2,
		Action:           domain.AdminRiskBlock,
		Reason:           "later independently authorized risk block",
	}); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil || replayed != first {
		t.Fatal("later risk revision rewrote or reapplied historical acceptance", err)
	}
	assertRiskControlState(ctx, t, riskControlFixture(t, db), actor, request.StrategyID, false, 3)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 2)
}

func TestAdminEmergencyScopeAuditOrAcceptanceFailureRollsBackRiskAndAllHistory(t *testing.T) {
	for _, receiptFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "scope audit", true: "acceptance"}[receiptFailure], func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			before := activationBudget(ctx, t, db, submit.Input.Authority.StrategyID)
			risk := riskEventSourceRow(ctx, t, db, before.StrategyInstanceId)
			injected := errors.New("test-only emergency persistence failure")
			const callback = "test:reject-emergency-storage"
			if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
				audit, isAudit := tx.Statement.Dest.(*modelv1.AdminAudit)
				_, isAcceptance := tx.Statement.Dest.(*modelv1.AdminEmergencyCancellation)
				if receiptFailure && isAcceptance || !receiptFailure && isAudit && audit.Action == domain.AdminEmergencyCancelAction {
					tx.Error = injected
				}
			}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := db.Callback().Create().Remove(callback); err != nil {
					t.Error(err)
				}
			})
			result, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(before.StrategyInstanceId))
			if !errors.Is(err, domain.ErrAdminControlUnavailable) || !errors.Is(err, injected) || result.ID != 0 {
				t.Fatal("failed scope persistence returned partial acceptance or lost its cause", err)
			}
			if !proto.Equal(before, activationBudget(ctx, t, db, before.StrategyInstanceId)) || !proto.Equal(risk, riskEventSourceRow(ctx, t, db, before.StrategyInstanceId)) {
				t.Fatal("scope persistence failure left risk disabled or cache changed")
			}
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
		})
	}
}

func emergencyAcceptanceStore(t *testing.T, db *gorm.DB) domain.AdminEmergencyCancellations {
	t.Helper()
	store, err := repository.NewAdminEmergencyCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func emergencyAcceptanceRequest(strategyID int64) domain.AdminEmergencyCancelRequest {
	return domain.AdminEmergencyCancelRequest{
		Key:                  "scoped-emergency-request-0001",
		Scope:                domain.AdminEmergencyCancelScopeStrategy,
		StrategyID:           strategyID,
		ExpectedRiskRevision: 1,
		Reason:               "explicitly block this strategy and schedule scoped cancellation",
	}
}

func assertEmergencyUnchangedBalances(ctx context.Context, t *testing.T, db *gorm.DB, accountID string, before []*modelv1.SimulatorBalance) {
	t.Helper()
	var after []*modelv1.SimulatorBalance
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ?", accountID).Order("id ASC").Find(&after).Error; err != nil || len(after) != len(before) {
		t.Fatal("scope acceptance changed core balance rows", err)
	}
	for index := range before {
		if !proto.Equal(before[index], after[index]) {
			t.Fatal("scope acceptance changed authoritative available/frozen funds")
		}
	}
}
