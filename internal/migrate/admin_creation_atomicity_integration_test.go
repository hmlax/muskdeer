//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminCreationConcurrentReplayAndChangedPayloadHaveOneEffect(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	request := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)
	type response struct {
		receipt domain.AdminCreationReceipt
		err     error
	}
	start := make(chan struct{})
	results := make(chan response, 2)
	for range 2 {
		go func() {
			<-start
			receipt, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
			results <- response{
				receipt: receipt,
				err:     err,
			}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.receipt.ID <= 0 || first.receipt.ID != second.receipt.ID {
		t.Fatal("concurrent creation did not converge to one receipt", first.err, second.err)
	}
	changed := request
	changed.Reason = "different reason under the original key"
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("creation replay accepted changed contents", err)
	}
	changed = request
	changed.Source = domain.MarketSourceBinance
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("creation replay silently changed source", err)
	}
	changed = request
	changed.Key = "different-create-key-0001"
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("duplicate business instance name created a second definition", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyCreation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyBudget{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminCreationExpiryAfterAuthorityCheckRollsBackAllMetadata(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	now := controlTestDatabaseTime(ctx, t, db, actor.Principal.UserID)
	token, tokenHash := controlTestSecret(21)
	csrf, csrfHash := controlTestSecret(22)
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
	const callback = "test:creation-expiry-before-receipt"
	delayed := false
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminStrategyCreation); !ok {
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
	request := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceBinance)
	if _, err := creator.Create(ctx, token, csrf, request); !errors.Is(err, domain.ErrUnauthenticated) || !delayed {
		t.Fatal("creation committed after initially valid authority expired", err)
	}
	if actual := controlTestDatabaseTime(ctx, t, db, actor.Principal.UserID); actual <= session.ExpiresAt {
		t.Fatal("expiry regression did not cross the actual database deadline")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyCreation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyBudget{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}
