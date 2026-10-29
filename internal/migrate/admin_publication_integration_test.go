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
	"github.com/hmlax/muskdeer/internal/strategyconfig"
)

func TestAdminPublicationPersistsBothTemplatesAndSourcesWithoutActivation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	var first domain.AdminPublicationReceipt
	var original domain.AdminPublishStrategy
	for _, template := range []domain.StrategyTemplate{domain.StrategyTemplateMarketMaker, domain.StrategyTemplateGrid} {
		for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
			creation := adminCreationRequest(t, submit, template, source)
			created, err := creator.Create(ctx, actor.Token, actor.CSRF, creation)
			if err != nil {
				t.Fatal(err)
			}
			before := publicationInstance(ctx, t, db, created.StrategyID)
			budget := activationBudget(ctx, t, db, created.StrategyID)
			request := publicationRequest(creation, created.StrategyID, before.Revision, before.RequestedConfigVersion)
			receipt, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request)
			if err != nil || receipt.ID <= 0 || receipt.ConfigVersion != 2 || receipt.PreviousConfigVersion != 1 || receipt.Revision != before.Revision+1 || receipt.PreviousRevision != before.Revision || receipt.ActorUserID != actor.Principal.UserID || receipt.Source != request.Source || receipt.Reason != request.Reason {
				t.Fatalf("publication did not commit exact typed requested input: %+v, %v", receipt, err)
			}
			assertPublicationEffect(ctx, t, db, before, budget, request, receipt)
			if first.ID == 0 {
				first, original = receipt, request
			}
			replayed, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request)
			if err != nil || replayed != receipt {
				t.Fatalf("publication replay changed original metadata receipt: %+v, %v", replayed, err)
			}
			changed := request
			changed.Reason += " changed"
			if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
				t.Fatal("publication reused a key with different content", err)
			}
			request.Key += "-stale"
			if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrAdminConflict) {
				t.Fatal("stale revision created another configuration", err)
			}
		}
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyPublication{}, 4)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyConfigVersion{}, 9)
	assertGatewayOrderCount(ctx, t, db, 0)
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).Where("id = ?", first.ID).UpdateColumn("reason", "rewrite").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).Where("id = ?", first.ID).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyPublication{}).Where("id = ?", first.ID).Delete(&modelv1.AdminStrategyPublication{}).Error, "23514")
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, original); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked session replayed a publication receipt", err)
	}
}

func TestAdminPublicationPreservesLiveObligationsAndSupersedesBoundControls(t *testing.T) {
	for _, withControl := range []bool{false, true} {
		name := "running"
		if withControl {
			name = "pending_stop"
		}
		t.Run(name, func(t *testing.T) {
			db, _ := migrationDatabase(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			actor, _, submit := adminCreationFixture(ctx, t, db)
			core, intents := executionFixture(t, db)
			submit.Input.Deadline = time.Now().Add(time.Second)
			live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
			if err != nil || live.State != domain.OrderStateLive {
				t.Fatalf("actual order was not funded: %+v, %v", live, err)
			}
			strategyID := submit.Input.Authority.StrategyID
			before := publicationInstance(ctx, t, db, strategyID)
			var operation domain.AdminOperation
			if withControl {
				controls, err := repository.NewAdminStrategyControl(db)
				if err != nil {
					t.Fatal(err)
				}
				operation, err = controls.Control(ctx, actor.Token, actor.CSRF, domain.AdminControlRequest{
					Key:              "publication-pending-stop-01",
					StrategyID:       strategyID,
					ExpectedRevision: before.Revision,
					Action:           domain.AdminControlStop,
					Reason:           "stop before configuration replacement",
				})
				if err != nil {
					t.Fatal(err)
				}
				before = publicationInstance(ctx, t, db, strategyID)
			}
			budget := activationBudget(ctx, t, db, strategyID)
			request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), strategyID, before.Revision, before.RequestedConfigVersion)
			publisher, err := repository.NewAdminStrategyPublisher(db)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request)
			if err != nil {
				t.Fatal(err)
			}
			assertPublicationEffect(ctx, t, db, before, budget, request, receipt)
			assertReservedBudget(ctx, t, db, submit.Input.AccountID, "200")
			observed, err := intents.ByID(ctx, submit.Input.AccountID, live.ID)
			if err != nil || observed.State != domain.OrderStateLive || observed.Revision != live.Revision {
				t.Fatal("publication mutated a live trading obligation", err)
			}
			var risk modelv1.RiskSnapshot
			if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", strategyID).First(&risk).Error; err != nil || risk.AllowNewRisk || risk.ReasonCode != string(domain.RiskReasonNotEvaluated) {
				t.Fatal("publication retained the old risk grant", err)
			}
			if withControl {
				var replaced modelv1.AdminStrategyOperation
				if err := db.WithContext(ctx).Model(&modelv1.AdminStrategyOperation{}).Where("id = ?", operation.ID).First(&replaced).Error; err != nil || replaced.State != string(domain.AdminOperationSuperseded) || replaced.GetObservedRevision() != receipt.Revision {
					t.Fatal("publication did not synchronously supersede the old bound control", err)
				}
			}
		})
	}
}

func publicationRequest(creation domain.AdminCreateStrategy, strategyID, revision, version int64) domain.AdminPublishStrategy {
	if creation.Parameters.Maker != nil {
		creation.Parameters.Maker.SideBudget = decimal.RequireFromString("35.000000000000000003")
		creation.Parameters.Maker.InventoryHardLimit = decimal.NewFromInt(20)
	} else {
		creation.Parameters.Grid.TotalBudget = decimal.RequireFromString("35.000000000000000003")
		creation.Parameters.Grid.InventoryLimit = decimal.NewFromInt(20)
	}
	source := domain.MarketSourceOKX
	if creation.Source == source {
		source = domain.MarketSourceBinance
	}
	return domain.AdminPublishStrategy{
		Key:                   "publish-" + creation.Key,
		StrategyID:            strategyID,
		ExpectedRevision:      revision,
		ExpectedConfigVersion: version,
		Source:                source,
		Parameters:            creation.Parameters,
		Reason:                "explicit configuration publication",
	}
}

func publicationInstance(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64) *modelv1.StrategyInstance {
	t.Helper()
	var row modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", strategyID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}

func assertPublicationEffect(ctx context.Context, t *testing.T, db *gorm.DB, before *modelv1.StrategyInstance, budget *modelv1.StrategyBudget, request domain.AdminPublishStrategy, receipt domain.AdminPublicationReceipt) {
	t.Helper()
	after := publicationInstance(ctx, t, db, before.Id)
	expected := proto.CloneOf(before)
	expected.RequestedConfigVersion, expected.MarketDataSource, expected.Revision, expected.UpdateAt = receipt.ConfigVersion, string(request.Source), receipt.Revision, after.UpdateAt
	if !proto.Equal(expected, after) || !proto.Equal(budget, activationBudget(ctx, t, db, before.Id)) {
		t.Fatal("publication changed identity, active runtime, target or live budget")
	}
	var version modelv1.StrategyConfigVersion
	if err := db.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Where("instance_id = ? AND version = ?", before.Id, receipt.ConfigVersion).First(&version).Error; err != nil {
		t.Fatal(err)
	}
	encoded, err := strategyconfig.Encode(request.Parameters)
	if err != nil || !proto.Equal(encoded.GetMarketMaker(), version.MakerParameters) || !proto.Equal(encoded.GetGrid(), version.GridParameters) || version.MarketDataSource != string(request.Source) || version.CodeVersion != before.CodeVersion {
		t.Fatal("publication lost typed precision or immutable version identity", err)
	}
}
