//go:build integration

package migrate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminPublicationConcurrentReplayAndCompetingKeysHaveOneEffect(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	type outcome struct {
		receipt domain.AdminPublicationReceipt
		err     error
	}
	results := make(chan outcome, 6)
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 6 {
		workers.Go(func() {
			<-start
			receipt, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request)
			results <- outcome{
				receipt: receipt,
				err:     err,
			}
		})
	}
	close(start)
	workers.Wait()
	var first domain.AdminPublicationReceipt
	for range 6 {
		result := <-results
		if result.err != nil || result.receipt.ID <= 0 {
			t.Fatal("concurrent identical publication failed", result.err)
		}
		if first.ID == 0 {
			first = result.receipt
		} else if result.receipt != first {
			t.Fatal("concurrent identical publication produced multiple receipts")
		}
	}
	next := request
	next.ExpectedRevision, next.ExpectedConfigVersion = first.Revision, first.ConfigVersion
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	const secondUsername = "publication-second-operator"
	if _, err := auth.Provision(ctx, "publication-test-setup", secondUsername, adminFixturePassword, domain.AdminRoleOperator); err != nil {
		t.Fatal(err)
	}
	secondActor, err := auth.Login(ctx, secondUsername, adminFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	start = make(chan struct{})
	for index, key := range []string{"publication-compete-0001", "publication-compete-0002"} {
		candidate := next
		candidate.Key = key
		identity := actor
		if index == 1 {
			identity = secondActor
		}
		workers.Go(func() {
			<-start
			receipt, err := publisher.Publish(ctx, identity.Token, identity.CSRF, candidate)
			results <- outcome{
				receipt: receipt,
				err:     err,
			}
		})
	}
	close(start)
	workers.Wait()
	winners, conflicts := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil && result.receipt.ConfigVersion == 3:
			winners++
		case errors.Is(result.err, domain.ErrAdminConflict):
			conflicts++
		default:
			t.Fatal("competing publication had an invalid result", result.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("competing optimistic publications did not have one effect")
	}
	if replayed, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); err != nil || replayed != first {
		t.Fatal("later publication changed the original receipt", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 3)
	var audits int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("action = ?", domain.AdminStrategyPublish).Count(&audits).Error; err != nil || audits != 2 {
		t.Fatal("concurrent publishers did not retain exactly two publication audits", err)
	}
}

func TestAdminPublicationAuditFailureAndRejectionsRollBackAllEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "publication-audit-pending-01",
		StrategyID:       before.Id,
		ExpectedRevision: before.Revision,
		Action:           domain.AdminControlPause,
		Reason:           "pause before failing publication",
	})
	if err != nil {
		t.Fatal(err)
	}
	before = publicationInstance(ctx, t, db, before.Id)
	var previousOperation modelv1.AdminStrategyOperation
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", pending.ID).First(&previousOperation).Error; err != nil {
		t.Fatal(err)
	}
	budget := activationBudget(ctx, t, db, before.Id)
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", before.Id).First(&risk).Error; err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	const callback = "test:publication-audit-rollback"
	injected := errors.New("controlled publication audit failure")
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminAudit); ok {
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
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, injected) {
		t.Fatal("publication did not preserve the failing audit cause", err)
	}
	candidateSource, candidateVersion := request, request
	candidateSource.Source = domain.MarketSourceBybit
	candidateVersion.ExpectedConfigVersion++
	for _, candidate := range []domain.AdminPublishStrategy{
		candidateSource,
		candidateVersion,
		publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion),
	} {
		if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, candidate); !errors.Is(err, domain.ErrInvalidAdminPublication) && !errors.Is(err, domain.ErrAdminConflict) {
			t.Fatal("invalid/stale/template-changing publication was accepted", err)
		}
	}
	if _, err := publisher.Publish(ctx, actor.Token, "", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("publication accepted missing CSRF", err)
	}
	var retainedRisk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", risk.Id).First(&retainedRisk).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(budget, activationBudget(ctx, t, db, before.Id)) || !proto.Equal(&risk, &retainedRisk) {
		t.Fatal("failed publication retained a partial requested target, budget or risk closure")
	}
	var retainedOperation modelv1.AdminStrategyOperation
	if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", pending.ID).First(&retainedOperation).Error; err != nil || !proto.Equal(&previousOperation, &retainedOperation) {
		t.Fatal("failed publication superseded the previous control permanently", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 1)
}

func TestAdminPublicationRequiresCurrentRoleAndExactSelectedFeed(t *testing.T) {
	for _, role := range []domain.AdminRole{domain.AdminRoleViewer, domain.AdminRoleOperator} {
		t.Run(string(role), func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var actor domain.AdminLogin
			var submit domain.GatewaySubmit
			want := domain.ErrForbidden
			_, submit = admissionFixture(ctx, t, db)
			actor = adminControlSessionFixtureForRole(ctx, t, db, role)
			if role == domain.AdminRoleOperator {
				portfolioFixture(ctx, t, db, submit)
				want = domain.ErrInvalidAdminPublication
			}
			before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
			request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
			publisher, err := repository.NewAdminStrategyPublisher(db)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, want) {
				t.Fatal("publication bypassed role or substituted another source", err)
			}
			if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
				t.Fatal("rejected publication changed the requested pair")
			}
			assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 0)
			assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 1)
		})
	}
}

func TestAdminPublicationExpiryBeforeReceiptRollsBackConfigurationAndRisk(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	now := controlTestDatabaseTime(ctx, t, db, actor.Principal.UserID)
	token, tokenHash := controlTestSecret(23)
	csrf, csrfHash := controlTestSecret(24)
	session := &modelv1.AdminSession{
		CreateAt:          now,
		UpdateAt:          now,
		UserId:            actor.Principal.UserID,
		TokenHash:         tokenHash,
		CsrfHash:          csrfHash,
		CredentialVersion: 1,
		ExpiresAt:         now + 1000,
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminSession{}).Create(session).Error; err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	budget := activationBudget(ctx, t, db, before.Id)
	const callback = "test:publication-expiry-before-receipt"
	delayed := false
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminStrategyPublication); !ok {
			return
		}
		delayed = true
		timer := time.NewTimer(1100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			tx.Error = ctx.Err()
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	if _, err := publisher.Publish(ctx, token, csrf, request); !errors.Is(err, domain.ErrUnauthenticated) || !delayed {
		t.Fatal("publication committed after the valid initial session expired", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(budget, activationBudget(ctx, t, db, before.Id)) {
		t.Fatal("expired publication retained partial state")
	}
	var risk modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", before.Id).First(&risk).Error; err != nil || !risk.AllowNewRisk || risk.Revision != 1 {
		t.Fatal("expired publication retained risk changes", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
}
