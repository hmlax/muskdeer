//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminStrategyQueriesKeepPaginationSourcesAndExactTypedVersions(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	reader, auth, login, instances := adminStrategiesFixture(ctx, t, db)
	page, err := reader.List(ctx, login.Token, domain.AdminPage{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].Spec.ID != instances[0].Id || page.NextAfterID != instances[0].Id {
		t.Fatalf("first page lost ascending identity: error=%v count=%d cursor=%d", err, len(page.Items), page.NextAfterID)
	}
	next, err := reader.List(ctx, login.Token, domain.AdminPage{
		AfterID: page.NextAfterID,
		Limit:   1,
	})
	if err != nil || len(next.Items) != 1 || next.Items[0].Spec.ID != instances[1].Id || next.NextAfterID != 0 {
		t.Fatal("second page omitted or repeated an instance", err)
	}
	for _, instance := range instances {
		detail, err := reader.Detail(ctx, login.Token, instance.Id)
		if err != nil {
			t.Fatal(err)
		}
		if detail.Strategy.Spec.RequestedSource != domain.MarketSource(instance.MarketDataSource) || detail.Requested.Source != detail.Strategy.Spec.RequestedSource || detail.Active != nil ||
			detail.Strategy.CreatedAt.IsZero() || detail.Strategy.UpdatedAt.IsZero() || detail.Requested.SchemaVersion != domain.ParametersSchemaVersion {
			t.Fatal("requested/source/version metadata was fabricated or incomplete")
		}
		assertAdminExactParameters(t, detail.Requested.Parameters, domain.StrategyTemplate(instance.Template))
		versions, err := reader.Versions(ctx, login.Token, instance.Id, domain.AdminPage{Limit: 1})
		if err != nil || len(versions.Items) != 1 || versions.Items[0].StrategyID != instance.Id || versions.Items[0].Source != detail.Requested.Source || versions.NextAfterID != 0 {
			t.Fatal("configuration list crossed instance/source identity", err)
		}
	}
	if err := auth.Logout(ctx, login.Token, login.CSRF); err != nil {
		t.Fatal(err)
	}
	assertAdminQueryDenied(ctx, t, reader, login.Token, instances[0].Id)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertAdminExactParameters(t *testing.T, parameters domain.StrategyParameters, template domain.StrategyTemplate) {
	t.Helper()
	switch template {
	case domain.StrategyTemplateMarketMaker:
		p := parameters.Maker
		if p == nil || parameters.Grid != nil || !p.LevelNotional.Equal(decimal.RequireFromString("123.123456789012345678")) {
			t.Fatal("maker decimal or typed union lost precision")
		}
	case domain.StrategyTemplateGrid:
		p := parameters.Grid
		if p == nil || parameters.Maker != nil || !p.LowerPrice.Equal(decimal.RequireFromString("0.123456789012345678")) {
			t.Fatal("grid decimal or typed union lost precision")
		}
	default:
		t.Fatal("unknown template")
	}
}

func TestAdminStrategyQueriesRejectBoundsMissingAndInvalidAuthority(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	reader, _, login, instances := adminStrategiesFixture(ctx, t, db)
	for _, page := range []domain.AdminPage{
		{Limit: 0},
		{Limit: domain.MaxAdminPageSize + 1},
		{
			AfterID: -1,
			Limit:   1,
		},
	} {
		if _, err := reader.List(ctx, login.Token, page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
			t.Fatal("unbounded/negative page accepted", err)
		}
		if _, err := reader.Versions(ctx, login.Token, instances[0].Id, page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
			t.Fatal("unbounded configuration page accepted", err)
		}
	}
	if _, err := reader.Detail(ctx, login.Token, 0); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nonpositive identity accepted", err)
	}
	missing := instances[1].Id + 1
	if _, err := reader.Detail(ctx, login.Token, missing); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing instance was not explicit", err)
	}
	if _, err := reader.Versions(ctx, login.Token, missing, domain.AdminPage{Limit: 1}); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("missing instance became an empty version list", err)
	}
	assertAdminQueryDenied(ctx, t, reader, "invalid-session", instances[0].Id)
	// A credential change must close the query boundary without relying on a
	// previous API identity check or a process-local principal cache.
	if err := db.WithContext(ctx).Model(&modelv1.AdminUser{}).Where("id = ?", login.Principal.UserID).
		Select("CredentialVersion", "Revision").Updates(&modelv1.AdminUser{
		CredentialVersion: 2,
		Revision:          2,
	}).Error; err != nil {
		t.Fatal(err)
	}
	assertAdminQueryDenied(ctx, t, reader, login.Token, instances[0].Id)
}

func TestAdminStrategyVersionsUseOneSnapshotDuringConcurrentVersionChange(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	reader, _, login, instances := adminStrategiesFixture(ctx, t, db)
	instance := instances[0]
	const callback = "test:admin-query-concurrent-version"
	changed := false
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		row, ok := query.Statement.Dest.(*modelv1.StrategyInstance)
		if !ok || row.Id != instance.Id || changed {
			return
		}
		changed = true
		// The independently committed write happens between the instance and
		// version-list reads. RR must exclude the newly inserted version.
		writer := db.Session(&gorm.Session{NewDB: true})
		err := writer.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			version := storedParameterFixture(domain.StrategyTemplateMarketMaker)
			version.InstanceId, version.Version = instance.Id, 2
			version.MarketDataSource = string(domain.MarketSourceOKX)
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(version).Error; err != nil {
				return err
			}
			return tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", instance.Id).
				Select("RequestedConfigVersion", "MarketDataSource", "ActiveConfigVersion", "ActiveMarketDataSource", "Revision").Updates(&modelv1.StrategyInstance{
				RequestedConfigVersion: 2,
				MarketDataSource:       string(domain.MarketSourceOKX),
				ActiveConfigVersion:    proto.Int64(1),
				ActiveMarketDataSource: proto.String(string(domain.MarketSourceBinance)),
				Revision:               2,
			}).Error
		})
		query.Error = errors.Join(query.Error, err)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	observed, err := reader.Versions(ctx, login.Token, instance.Id, domain.AdminPage{Limit: domain.MaxAdminPageSize})
	if err != nil || !changed || len(observed.Items) != 1 || observed.Items[0].Version != 1 || observed.Items[0].Source != domain.MarketSourceBinance {
		t.Fatal("version list mixed observations from different database snapshots", err)
	}
	current, err := reader.Detail(ctx, login.Token, instance.Id)
	if err != nil || current.Strategy.Spec.RequestedVersion != 2 || current.Requested.Version != 2 || current.Requested.Source != domain.MarketSourceOKX {
		t.Fatal("new query failed to observe committed version/source", err)
	}
	if current.Active == nil || current.Active.Version != 1 || current.Active.Source != domain.MarketSourceBinance || current.Active.Parameters.Maker == nil {
		t.Fatal("requested source was silently substituted for the active source")
	}
	versions, err := reader.Versions(ctx, login.Token, instance.Id, domain.AdminPage{Limit: 1})
	if err != nil || len(versions.Items) != 1 || versions.NextAfterID <= 0 {
		t.Fatal("multiple versions were not bounded", err)
	}
	next, err := reader.Versions(ctx, login.Token, instance.Id, domain.AdminPage{
		AfterID: versions.NextAfterID,
		Limit:   1,
	})
	if err != nil || len(next.Items) != 1 || next.Items[0].Version != 2 || next.NextAfterID != 0 {
		t.Fatal("version cursor repeated or skipped the new version", err)
	}
}

func adminStrategiesFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminStrategies, domain.AdminAuth, domain.AdminLogin, []*modelv1.StrategyInstance) {
	t.Helper()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	instances := []*modelv1.StrategyInstance{
		identityInstance(instrument.Id, "query-maker", domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance),
		identityInstance(instrument.Id, "query-grid", domain.StrategyTemplateGrid, domain.MarketSourceOKX),
	}
	for _, instance := range instances {
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(instance).Error; err != nil {
				return err
			}
			parameters := storedParameterFixture(domain.StrategyTemplate(instance.Template))
			parameters.InstanceId = instance.Id
			parameters.MarketDataSource = instance.MarketDataSource
			if parameters.MakerParameters != nil {
				parameters.MakerParameters.LevelNotional = "123.123456789012345678"
				parameters.MakerParameters.SideBudget = "1000"
			} else {
				parameters.GridParameters.LowerPrice = "0.123456789012345678"
			}
			return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminProvisionRequest(ctx, t, auth, "strategy-viewer", domain.AdminRoleViewer); err != nil {
		t.Fatal(err)
	}
	login, err := adminLoginRequest(ctx, t, auth, "strategy-viewer", adminFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := repository.NewAdminStrategies(db)
	if err != nil {
		t.Fatal(err)
	}
	return reader, auth, login, instances
}

func assertAdminQueryDenied(ctx context.Context, t *testing.T, reader domain.AdminStrategies, token string, id int64) {
	t.Helper()
	if _, err := reader.List(ctx, token, domain.AdminPage{Limit: 1}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("unauthenticated strategy list succeeded", err)
	}
	if _, err := reader.Detail(ctx, token, id); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("unauthenticated strategy detail succeeded", err)
	}
	if _, err := reader.Versions(ctx, token, id, domain.AdminPage{Limit: 1}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("unauthenticated version list succeeded", err)
	}
}
