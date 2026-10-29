//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminOrdersCancellationSnapshotsDoNotReleaseBeforeOMSConfirmation(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	pending, err := cancels.Cancel(ctx, cancelInputFixture(ctx, submit, live.ClientOrderID, "admin-order-pending-cancel-01"), submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal(err)
	}
	reader, err := repository.NewAdminOrders(db)
	if err != nil {
		t.Fatal(err)
	}
	id := submit.Input.Authority.StrategyID
	assertAdminOrderPendingCancellation(ctx, t, reader, actor.Token, id, live.ID)
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	assertAdminOrderCanceledBeforeOMS(ctx, t, reader, actor.Token, id, live.ID)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	if result, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil || result.Request.State != domain.CancellationConfirmed {
		t.Fatal(err)
	}
	after, err := reader.Detail(ctx, actor.Token, id, live.ID)
	if err != nil || after.State != domain.OrderStateCanceled || after.CancelState != domain.CancelStateConfirmed || after.Core == nil || after.ConfirmedCoreID != after.Core.ID || after.ConfirmedCoreRevision != after.Core.Revision || !after.ReservedNotional.IsZero() {
		t.Fatal("new read lost actual OMS terminal confirmation", err)
	}
	page, err := reader.List(ctx, actor.Token, domain.AdminOrderFilter{
		StrategyID: id,
		Page:       domain.AdminPage{Limit: 1},
		OpenOnly:   true,
	})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextAfterID != 0 {
		t.Fatal("confirmed terminal cancellation remained an open obligation", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 0)
	assertGatewayOrderCount(ctx, t, db, 1)
}

func assertAdminOrderPendingCancellation(ctx context.Context, t *testing.T, reader domain.AdminOrders, token string, strategyID, orderID int64) {
	t.Helper()
	before, err := reader.Detail(ctx, token, strategyID, orderID)
	if err != nil || before.CancelState != domain.CancelStatePending || before.Core == nil || before.Core.CancelState != domain.CancelStatePending || before.ReservedNotional.String() != "200" || before.Core.FrozenAmount.String() != "200.2" {
		t.Fatal("pending cancellation was hidden or treated as terminal", err)
	}
}

func assertAdminOrderCanceledBeforeOMS(ctx context.Context, t *testing.T, reader domain.AdminOrders, token string, strategyID, orderID int64) {
	t.Helper()
	gap, err := reader.Detail(ctx, token, strategyID, orderID)
	if err != nil || gap.State != domain.OrderStateLive || gap.CancelState != domain.CancelStatePending || gap.Core == nil || gap.Core.State != domain.OrderStateCanceled || gap.Core.CancelState != domain.CancelStateConfirmed || !gap.Core.FrozenAmount.IsZero() || gap.ReservedNotional.String() != "200" {
		t.Fatal("read silently reconciled core cancellation or released OMS reservations", err)
	}
}
