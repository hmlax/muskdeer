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
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

func TestAdminCreationPersistsBothTemplatesAndSourcesWithoutTradingEffects(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	var first domain.AdminCreationReceipt
	var original domain.AdminCreateStrategy
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			request := adminCreationRequest(t, submit, template, source)
			receipt, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
			if err != nil || receipt.ID <= 0 || receipt.StrategyID <= 0 || receipt.ActorUserID != actor.Principal.UserID || receipt.ConfigVersion != 1 || receipt.Revision != 1 || receipt.Source != source {
				t.Fatal("creation did not persist the exact audited initial definition", err)
			}
			assertCreatedStrategy(ctx, t, db, request, receipt)
			again, err := creator.Create(ctx, actor.Token, actor.CSRF, request)
			if err != nil || again.ID != receipt.ID || again.StrategyID != receipt.StrategyID {
				t.Fatal("creation replay produced another effect", err)
			}
			if first.ID == 0 {
				first, original = receipt, request
			}
		}
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyCreation{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 5)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyBudget{}, 5)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 4)
	controls, err := repository.NewAdminStrategyControl(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
		Key:              "created-strategy-start-0001",
		StrategyID:       first.StrategyID,
		ExpectedRevision: 1,
		Action:           domain.AdminControlStart,
		Reason:           "later explicit run request",
	}); err != nil {
		t.Fatal(err)
	}
	replayed, err := creator.Create(ctx, actor.Token, actor.CSRF, original)
	if err != nil || replayed.ID != first.ID || replayed.Revision != 1 {
		t.Fatal("later runtime control changed the original creation receipt", err)
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).Where("id = ?", first.ID).UpdateColumn("reason", "rewrite").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyCreation{}).Where("id = ?", first.ID).Delete(&modelv1.AdminStrategyCreation{}).Error, "23514")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func TestAdminCreationAuditFailureAndRejectedRequestsLeaveNoDefinition(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	request := adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX)
	if _, err := creator.Create(ctx, actor.Token, "invalid-csrf", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("creation accepted invalid CSRF", err)
	}
	bad := request
	bad.Source = domain.MarketSourceBybit
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, bad); !errors.Is(err, domain.ErrInvalidAdminCreation) {
		t.Fatal("creation enabled a candidate source", err)
	}
	bad = request
	bad.Mode = domain.MarketModeLive
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, bad); !errors.Is(err, domain.ErrInvalidAdminCreation) {
		t.Fatal("creation silently used REPLAY for absent LIVE binding", err)
	}
	const callback = "test:reject-creation-audit"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*modelv1.AdminAudit); ok {
			tx.Error = errors.New("test-only audit storage failure")
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	if _, err := creator.Create(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrAdminControlUnavailable) {
		t.Fatal("audit failure became a successful creation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyCreation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyBudget{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
}

func adminCreationFixture(ctx context.Context, t *testing.T, db *gorm.DB) (domain.AdminLogin, domain.AdminStrategyCreator, domain.GatewaySubmit) {
	t.Helper()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	actor := adminControlSessionFixture(ctx, t, db)
	feeds, err := repository.NewMarketFeeds(db)
	if err != nil {
		t.Fatal(err)
	}
	feed, err := feeds.Find(ctx, domain.MarketSourceBinance, submit.Input.InstrumentID, domain.MarketModeReplay)
	if err != nil {
		t.Fatal(err)
	}
	feed.ID, feed.Source, feed.Symbol = 0, domain.MarketSourceOKX, "BTC-USDT"
	if _, err := feeds.Create(ctx, feed); err != nil {
		t.Fatal(err)
	}
	creator, err := repository.NewAdminStrategyCreator(db)
	if err != nil {
		t.Fatal(err)
	}
	return actor, creator, submit
}

func adminCreationRequest(t *testing.T, submit domain.GatewaySubmit, template domain.StrategyTemplate, source domain.MarketSource) domain.AdminCreateStrategy {
	t.Helper()
	stored := storedParameterFixture(template)
	parameters := domain.StrategyParameters{Template: template}
	if template == domain.StrategyTemplateGrid {
		value, err := strategyconfig.Grid(stored.GridParameters)
		if err != nil {
			t.Fatal(err)
		}
		parameters.Grid = &value
	} else {
		value, err := strategyconfig.Maker(stored.MakerParameters)
		if err != nil {
			t.Fatal(err)
		}
		parameters.Maker = &value
	}
	return domain.AdminCreateStrategy{
		Key:          "strategy-create-" + string(template) + "-" + string(source),
		InstanceKey:  "created-" + string(template) + "-" + string(source),
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		CodeVersion:  identityFixtureCode,
		Source:       source,
		Mode:         domain.MarketModeReplay,
		Parameters:   parameters,
		Reason:       "explicit creation " + string(template) + " " + string(source),
	}
}

func assertCreatedStrategy(ctx context.Context, t *testing.T, db *gorm.DB, request domain.AdminCreateStrategy, receipt domain.AdminCreationReceipt) {
	t.Helper()
	var row modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", receipt.StrategyID).Take(&row).Error; err != nil || row.InstanceKey != request.InstanceKey || row.State != string(domain.StrategyStateCreated) || row.DesiredState != string(domain.StrategyStateStopped) || row.DesiredRunning || row.RuntimeOwnerEpoch != 0 || row.ActiveConfigVersion != nil || row.ActiveMarketDataSource != nil {
		t.Fatal("creation granted running/owner or lost initial identity", err)
	}
	var config modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ?", row.Id, 1).Take(&config).Error; err != nil || config.MarketDataSource != string(request.Source) {
		t.Fatal("creation lost initial selected configuration", err)
	}
	encoded, err := strategyconfig.Encode(request.Parameters)
	if err != nil || !proto.Equal(encoded.GetMarketMaker(), config.MakerParameters) || !proto.Equal(encoded.GetGrid(), config.GridParameters) {
		t.Fatal("creation changed typed decimal parameters", err)
	}
	var budget modelv1.StrategyBudget
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", row.Id).Take(&budget).Error; err != nil || !budget.Enabled {
		t.Fatal("creation reserved risk or omitted the independent budget", err)
	}
	reserved, err := domain.ParseLedgerDecimal(budget.ReservedNotional, false, true)
	if err != nil || !reserved.IsZero() {
		t.Fatal("creation reserved risk", err)
	}
	var audit modelv1.AdminAudit
	if err := db.WithContext(ctx).Model(&modelv1.AdminAudit{}).Where("resource = ? AND action = ?", domain.AdminAuditResourceStrategy+":"+strconv.FormatInt(row.Id, 10), domain.AdminStrategyCreate).Take(&audit).Error; err != nil || audit.GetAfterVersion() != 1 || audit.BeforeVersion != nil || audit.Reason != request.Reason {
		t.Fatal("creation omitted its exact immutable audit", err)
	}
}
