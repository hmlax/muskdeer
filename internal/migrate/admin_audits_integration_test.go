//go:build integration

package migrate

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminAuditsReadActualManagementHistoryWithExactFiltersAndPagination(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	reader, err := repository.NewAdminAudits(db)
	if err != nil {
		t.Fatal(err)
	}
	filter := domain.AdminAuditFilter{Page: domain.AdminPage{Limit: 1}}
	empty, err := reader.List(ctx, actor.Token, filter)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextAfterID != 0 {
		t.Fatal("empty audit query invented history", err)
	}
	creation := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceBinance)
	creation.Reason = "审计创建 <script> remains literal"
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, creation)
	if err != nil {
		t.Fatal(err)
	}
	other := adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceOKX)
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, other); err != nil {
		t.Fatal(err)
	}
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	publication := publicationRequest(creation, created.StrategyID, created.Revision, 1)
	receipt, err := publisher.Publish(ctx, actor.Token, actor.CSRF, publication)
	if err != nil {
		t.Fatal(err)
	}
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		t.Fatal(err)
	}
	control := domain.AdminControlRequest{
		Key:              "audit-control-request-0001",
		StrategyID:       created.StrategyID,
		ExpectedRevision: receipt.Revision,
		Action:           domain.AdminControlStart,
		Reason:           "audited requested start, not runtime completion",
	}
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, control); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, publication); err != nil {
		t.Fatal(err)
	}
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, control); err != nil {
		t.Fatal(err)
	}
	resource := domain.AdminAuditResourceStrategy + ":" + strconv.FormatInt(created.StrategyID, 10)
	filter.Actor, filter.Resource = actor.Principal.Username, resource
	entries := readAdminAuditPages(ctx, t, reader, actor.Token, filter)
	assertAdminAuditMeaning(t, entries, creation, receipt, control)
	assertAdminAuditDetails(ctx, t, reader, actor, resource, entries)
	filter.Page.AfterID, filter.Action = 0, domain.AdminStrategyPublish
	only, err := reader.List(ctx, actor.Token, filter)
	if err != nil || len(only.Items) != 1 || only.Items[0].ID != entries[1].ID || only.NextAfterID != 0 {
		t.Fatal("combined audit filters mixed unrelated actions", err)
	}
	filter.Actor = "another-actor"
	unrelated, err := reader.List(ctx, actor.Token, filter)
	if err != nil || len(unrelated.Items) != 0 {
		t.Fatal("audit actor filter was ignored", err)
	}
	filter.Actor = actor.Principal.Username
	filter.Resource = "%_';--"
	literal, err := reader.List(ctx, actor.Token, filter)
	if err != nil || literal.Items == nil || len(literal.Items) != 0 {
		t.Fatal("audit exact filter became a SQL expression or wildcard", err)
	}
	if _, err := reader.Detail(ctx, actor.Token, entries[2].ID+100); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing audit detail fabricated a result", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 1)
	assertGatewayOrderCount(ctx, t, db, 0)
	instance := publicationInstance(ctx, t, db, created.StrategyID)
	if instance.State != string(domain.StrategyStateCreated) || instance.RuntimeOwnerEpoch != 0 || instance.ActiveConfigVersion != nil {
		t.Fatal("audit reads granted runtime authority or completed an operation")
	}
}

func readAdminAuditPages(ctx context.Context, t *testing.T, reader domain.AdminAudits, token string, filter domain.AdminAuditFilter) []domain.AdminAuditEntry {
	t.Helper()
	var entries []domain.AdminAuditEntry
	for {
		page, err := reader.List(ctx, token, filter)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID <= filter.Page.AfterID {
			t.Fatal("exact audit page lost or repeated a committed record", err)
		}
		entries = append(entries, page.Items...)
		if page.NextAfterID == 0 {
			return entries
		}
		if len(entries) >= 3 || page.NextAfterID != page.Items[0].ID {
			t.Fatal("audit cursor did not advance at the returned row")
		}
		filter.Page.AfterID = page.NextAfterID
	}
}

func assertAdminAuditMeaning(t *testing.T, entries []domain.AdminAuditEntry, creation domain.AdminCreateStrategy, receipt domain.AdminPublicationReceipt, control domain.AdminControlRequest) {
	t.Helper()
	if len(entries) != 3 || entries[0].Action != domain.AdminStrategyCreate || entries[0].BeforeVersion != nil || entries[0].AfterVersion == nil || *entries[0].AfterVersion != 1 || entries[0].Reason != creation.Reason ||
		entries[1].Action != domain.AdminStrategyPublish || entries[1].BeforeVersion == nil || *entries[1].BeforeVersion != receipt.PreviousRevision || entries[1].AfterVersion == nil || *entries[1].AfterVersion != receipt.Revision ||
		entries[2].Action != string(domain.AdminControlStart) || entries[2].Reason != control.Reason {
		t.Fatalf("audit query changed historical meaning: %+v", entries)
	}
}

func assertAdminAuditDetails(ctx context.Context, t *testing.T, reader domain.AdminAudits, actor domain.AdminLogin, resource string, entries []domain.AdminAuditEntry) {
	t.Helper()
	for _, entry := range entries {
		if entry.Actor != actor.Principal.Username || entry.ActorRole != domain.AdminRoleOperator || entry.Resource != resource || entry.CreatedAt.IsZero() || entry.CreatedAt.Location() != time.UTC {
			t.Fatal("audit metadata lost actor, scope or UTC time")
		}
		detail, err := reader.Detail(ctx, actor.Token, entry.ID)
		if err != nil || detail.ID != entry.ID || detail.Action != entry.Action || detail.Resource != resource || detail.Reason != entry.Reason {
			t.Fatal("audit detail disagreed with its committed list record", err)
		}
	}
}

func TestAdminAuditsKeepViewerAuthorityAndHistoryInOneSnapshot(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	reader, err := repository.NewAdminAudits(db)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:audit-concurrent-record"
	inserted := false
	row := &modelv1.AdminAudit{
		Actor:     actor.Principal.Username,
		ActorRole: string(domain.AdminRoleViewer),
		Action:    domain.AdminAuditActionLogin,
		Resource:  domain.AdminAuditResourceSession + ":" + strconv.FormatInt(actor.Principal.SessionID, 10),
		Reason:    domain.AdminAuditReasonLogin,
	}
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if _, ok := query.Statement.Dest.(*modelv1.AdminSession); !ok || inserted {
			return
		}
		inserted = true
		if err := db.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(row).Error; err != nil {
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
	filter := domain.AdminAuditFilter{Page: domain.AdminPage{Limit: 1}}
	before, err := reader.List(ctx, actor.Token, filter)
	if err != nil || !inserted || len(before.Items) != 0 {
		t.Fatal("audit inserted after authority lookup escaped the RR snapshot", err)
	}
	after, err := reader.List(ctx, actor.Token, filter)
	if err != nil || len(after.Items) != 1 || after.Items[0].ID != row.Id {
		t.Fatal("viewer could not read the next committed audit snapshot", err)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", row.Id).UpdateColumn("reason", "rewrite").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("id = ?", row.Id).Delete(&modelv1.AdminAudit{}).Error, "23514")
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	assertAdminAuditsDenied(ctx, t, reader, actor.Token, row.Id)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func TestAdminAuditsRejectCurrentCredentialChangeAndKeepStoredVersionsOwned(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	row := &modelv1.AdminAudit{
		Actor:         actor.Principal.Username,
		ActorRole:     string(domain.AdminRoleViewer),
		Action:        domain.AdminAuditActionLogin,
		Resource:      domain.AdminAuditResourceUser,
		Reason:        domain.AdminAuditReasonLogin,
		BeforeVersion: proto.Int64(9007199254740993),
		AfterVersion:  proto.Int64(9007199254740994),
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	reader, err := repository.NewAdminAudits(db)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := reader.Detail(ctx, actor.Token, row.Id)
	if err != nil || entry.BeforeVersion == nil || *entry.BeforeVersion != *row.BeforeVersion || entry.AfterVersion == nil || *entry.AfterVersion != *row.AfterVersion {
		t.Fatal("audit detail lost int64 revision precision", err)
	}
	*entry.AfterVersion = 1
	again, err := reader.Detail(ctx, actor.Token, row.Id)
	if err != nil || again.AfterVersion == nil || *again.AfterVersion != *row.AfterVersion {
		t.Fatal("caller mutation changed persisted audit history", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", actor.Principal.UserID).
		Select("CredentialVersion", "Revision").Updates(&modelv1.AdminUser{
		CredentialVersion: 2,
		Revision:          2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	assertAdminAuditsDenied(ctx, t, reader, actor.Token, row.Id)
}

func TestAdminAuditsReturnNoPartialHistoryOnStorageFailureOrCancellation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	reader, err := repository.NewAdminAudits(db)
	if err != nil {
		t.Fatal(err)
	}
	const callback = "test:audit-query-failure"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		switch query.Statement.Dest.(type) {
		case *[]*modelv1.AdminAudit, *modelv1.AdminAudit:
			_ = query.AddError(errors.New("private persistence diagnostics"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	filter := domain.AdminAuditFilter{Page: domain.AdminPage{Limit: 1}}
	if page, err := reader.List(ctx, actor.Token, filter); !errors.Is(err, domain.ErrAdminUnavailable) || err.Error() != domain.MessageAdminUnavailable || len(page.Items) != 0 || page.NextAfterID != 0 {
		t.Fatal("audit query leaked a storage failure or partial result", err)
	}
	if entry, err := reader.Detail(ctx, actor.Token, 1); !errors.Is(err, domain.ErrAdminUnavailable) || err.Error() != domain.MessageAdminUnavailable || entry.ID != 0 {
		t.Fatal("audit detail leaked a storage failure or partial result", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := reader.List(canceled, actor.Token, filter); !errors.Is(err, context.Canceled) {
		t.Fatal("audit query ignored parent cancellation", err)
	}
	assertAdminAuditsDenied(ctx, t, reader, "invalid", 1)
}

func assertAdminAuditsDenied(ctx context.Context, t *testing.T, reader domain.AdminAudits, token string, id int64) {
	t.Helper()
	if page, err := reader.List(ctx, token, domain.AdminAuditFilter{Page: domain.AdminPage{Limit: 1}}); !errors.Is(err, domain.ErrUnauthenticated) || len(page.Items) != 0 {
		t.Fatal("audit list trusted stale authority or returned partial data", err)
	}
	if entry, err := reader.Detail(ctx, token, id); !errors.Is(err, domain.ErrUnauthenticated) || entry.ID != 0 {
		t.Fatal("audit detail trusted stale authority or returned partial data", err)
	}
}
