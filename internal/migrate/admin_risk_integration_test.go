//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminRiskBlockRejectsManagedDirectCoreSubmissions(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatal("risk test did not establish an actual funded order", err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	control, err := repository.NewAdminStrategyRisk(db)
	if err != nil {
		t.Fatal(err)
	}
	request := domain.AdminRiskRequest{
		Key:              "risk-block-live-core-0001",
		StrategyID:       before.Id,
		ExpectedRevision: 1,
		Action:           domain.AdminRiskBlock,
		Reason:           "close new risk while preserving funded obligations",
	}
	receipt, err := control.Change(ctx, actor.Token, actor.CSRF, request)
	if err != nil || receipt.Enabled || !receipt.PreviousEnabled || receipt.Revision != 2 {
		t.Fatal("risk block did not commit its exact scoped receipt", err)
	}
	after := publicationInstance(ctx, t, db, before.Id)
	if !proto.Equal(before, after) {
		t.Fatal("risk block changed runtime target, config or ownership")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	state, err := control.State(ctx, actor.Token, before.Id)
	if err != nil || state.Enabled || state.Revision != 2 || state.ReservedNotional.String() != "200" {
		t.Fatal("risk read lost the flag, independent revision or old reservation", err)
	}
	// Deliberately bypass OMS inside the test: the authoritative simulator must
	// still honor a managed strategy's disabled flag before committing funds.
	direct := submit
	direct.ClientOrderID = "risk-block-bypassed-client"
	direct.Input.IdempotencyKey = "risk-block-bypassed-core"
	direct.Input.Deadline = time.Now().Add(time.Second)
	if _, err := core.Submit(ctx, direct); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatal("managed risk block was bypassed by a direct core submit", err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 1)
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Where("id = ?", receipt.ID).UpdateColumn("reason", "rewritten history").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Where("id = ?", receipt.ID).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.AdminStrategyRiskChange{}).Where("id = ?", receipt.ID).Delete(&modelv1.AdminStrategyRiskChange{}).Error, "23514")
}
