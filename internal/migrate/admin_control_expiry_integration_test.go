//go:build integration

package migrate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type controlExpiryMarker struct{}

func TestAdminControlExpiryWhileWaitingForStrategyCannotCommit(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor, controls := adminControlFixture(ctx, t, db)
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instance := identityInstance(instrument.Id, "expiry-control", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = instance.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	token, tokenHash := controlTestSecret(11)
	csrf, csrfHash := controlTestSecret(12)
	now := controlTestDatabaseTime(ctx, t, db, actor.Principal.UserID)
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
	locking := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Begin()
	if locking.Error != nil {
		t.Fatal(locking.Error)
	}
	released := false
	defer func() {
		if !released {
			if err := locking.Rollback().Error; err != nil {
				t.Error(err)
			}
		}
	}()
	var held modelv1.StrategyInstance
	if err := locking.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Take(&held).Error; err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	var once sync.Once
	const callback = "test:control-expiry-resource-lock"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		_, strategy := tx.Statement.Dest.(*modelv1.StrategyInstance)
		if strategy && tx.Statement.Context.Value(controlExpiryMarker{}) == true {
			once.Do(func() { close(entered) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	call, stop := context.WithTimeout(context.WithValue(ctx, controlExpiryMarker{}, true), 3*time.Second)
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		defer close(finished)
		_, err := controls.Control(call, token, csrf, domain.AdminControlRequest{
			Key:              "management-expiry-0001",
			StrategyID:       instance.Id,
			ExpectedRevision: 1,
			Action:           domain.AdminControlStart,
			Reason:           "must remain authorized at effect",
		})
		result <- err
	}()
	defer func() { stop(); <-finished }()
	select {
	case <-entered:
	case <-call.Done():
		t.Fatal("control did not pass initial authentication before resource wait")
	}
	ticks := time.NewTicker(10 * time.Millisecond)
	defer ticks.Stop()
	for controlTestDatabaseTime(ctx, t, db, actor.Principal.UserID) < session.ExpiresAt {
		select {
		case <-ticks.C:
		case <-call.Done():
			t.Fatal("expiry proof exceeded original native operation budget")
		}
	}
	err := locking.Commit().Error
	released = true
	if err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("session expired during resource wait still committed", err)
	}
	assertControlInstance(ctx, t, db, instance.Id, domain.StrategyStateCreated, domain.StrategyStateStopped, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 0)
	var audits int64
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("action = ?", domain.AdminControlStart).Count(&audits).Error; err != nil || audits != 0 {
		t.Fatal("expired management request left an audit/effect", err)
	}
}

func controlTestSecret(seed byte) (string, string) {
	value := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
	digest := sha256.Sum256([]byte(value))
	return value, hex.EncodeToString(digest[:])
}

func controlTestDatabaseTime(ctx context.Context, t *testing.T, db *gorm.DB, userID int64) int64 {
	t.Helper()
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", userID).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	return clock.NowMS
}
