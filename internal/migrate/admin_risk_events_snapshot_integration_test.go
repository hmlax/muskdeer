//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskEventsAuthorityAndHistoryShareRepeatableSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	const callback = "test:risk-event-after-authority"
	changed := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, session := query.Statement.Dest.(*modelv1.AdminSession)
		if !session || changed {
			return
		}
		changed = true
		if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).
			Select("AllowNewRisk", "ReasonCode", "Revision").Updates(&modelv1.RiskSnapshot{
			AllowNewRisk: false,
			ReasonCode:   string(domain.RiskReasonBudget),
			Revision:     source.Revision + 1,
		}).Error; err != nil {
			_ = query.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	store := adminRiskEventsStore(t, db)
	filter := domain.AdminRiskEventFilter{Page: domain.AdminPage{Limit: 10}}
	page, err := store.List(ctx, actor.Token, filter)
	if err != nil || !changed || len(page.Items) != 1 || page.Items[0].Reason != domain.RiskReasonNotEvaluated {
		t.Fatal("concurrent source change escaped the authority snapshot", err)
	}
	next, err := store.List(ctx, actor.Token, filter)
	if err != nil || len(next.Items) != 2 || next.Items[1].Reason != domain.RiskReasonBudget || next.Items[1].RecordedAllowNewRisk {
		t.Fatal("next history snapshot lost the committed producer change", err)
	}
}

func TestAdminRiskEventsStorageFailureCancellationAndRevocationReturnNothing(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	row := riskEventRows(ctx, t, db, submit.Input.Authority.StrategyID)[0]
	const callback = "test:risk-event-private-read-failure"
	injected := errors.New("private risk history storage failure")
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(query *gorm.DB) {
		switch query.Statement.Dest.(type) {
		case *[]*modelv1.RiskSnapshotEvent, *modelv1.RiskSnapshotEvent:
			_ = query.AddError(injected)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	store := adminRiskEventsStore(t, db)
	filter := domain.AdminRiskEventFilter{Page: domain.AdminPage{Limit: 1}}
	page, err := store.List(ctx, actor.Token, filter)
	if !errors.Is(err, domain.ErrAdminUnavailable) || !errors.Is(err, injected) || err.Error() != domain.MessageAdminUnavailable || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("private failure escaped or returned partial history", err)
	}
	if detail, err := store.Detail(ctx, actor.Token, row.Id); !errors.Is(err, domain.ErrAdminUnavailable) || detail.ID != 0 {
		t.Fatal("failed history detail returned success data", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if page, err := store.List(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) || page.Items != nil {
		t.Fatal("history query ignored parent cancellation", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if page, err := store.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || page.Items != nil {
		t.Fatal("revoked session read history", err)
	}
	if detail, err := store.Detail(ctx, actor.Token, row.Id); !errors.Is(err, domain.ErrUnauthenticated) || detail.ID != 0 {
		t.Fatal("revoked session read history detail", err)
	}
}

func TestAdminRiskEventsReadSnapshotCannotChangeSourceOrHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	before := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	const callback = "test:risk-event-read-only"
	attempted := false
	var writeErr error
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		_, history := query.Statement.Dest.(*[]*modelv1.RiskSnapshotEvent)
		if !history || attempted {
			return
		}
		attempted = true
		writeErr = query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.RiskSnapshot{}).
			Where("id = ?", before.Id).Update("allow_new_risk", false).Error
		_ = query.AddError(writeErr)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	page, err := adminRiskEventsStore(t, db).List(ctx, actor.Token, domain.AdminRiskEventFilter{Page: domain.AdminPage{Limit: 1}})
	if !attempted || !errors.Is(err, domain.ErrAdminUnavailable) || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("history read allowed risk mutation or partial success", err)
	}
	assertDatabaseCode(t, writeErr, "25006")
	if !proto.Equal(before, riskEventSourceRow(ctx, t, db, before.StrategyInstanceId)) {
		t.Fatal("read changed risk source")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshotEvent{}, 1)
}

func TestAdminRiskEventsMalformedReadDropsPartialPageAndCursor(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).Update("allow_new_risk", false).Error; err != nil {
		t.Fatal(err)
	}
	const callback = "test:risk-event-malformed-read"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if rows, history := query.Statement.Dest.(*[]*modelv1.RiskSnapshotEvent); history && len(*rows) != 0 {
			// Response corruption only; immutable persisted rows are not modified.
			(*rows)[0].ReasonCode = "undefined"
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	page, err := adminRiskEventsStore(t, db).List(ctx, actor.Token, domain.AdminRiskEventFilter{Page: domain.AdminPage{Limit: 1}})
	if !errors.Is(err, domain.ErrAdminUnavailable) || page.Items != nil || page.NextAfterID != 0 {
		t.Fatal("malformed history returned a partial successful page", err)
	}
}
