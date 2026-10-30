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

func TestAdminCancellationWaitsForOMSAndRetainsExactOrderScope(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, _, _ := adminOrderFacts(ctx, t, db, core, intents, submit)
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := repository.NewStrategyOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	request := adminCancelRequest(live)
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil || receipt.ID <= 0 || receipt.State != domain.AdminCancellationAccepted || !receipt.CompletedAt.IsZero() {
		t.Fatal("explicit cancellation was not durably accepted", err)
	}
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	wrong := request
	wrong.Key, wrong.StrategyID = "admin-cancel-wrong-scope", other.StrategyID
	if _, err := control.Request(ctx, actor.Token, actor.CSRF, wrong); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("management cancellation crossed sibling scope", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("order cancellation changed lifecycle, configuration or ownership")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "789.79", "210.21")
	work, err := worker.Prepare(ctx, submit.Input.Authority, 0)
	if err != nil || work.ReceiptID != receipt.ID || work.Action == nil || work.Action.Plan.TargetIntentID != live.ID {
		t.Fatal("current owner did not get the scoped pending action", err)
	}
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	native, stop := context.WithTimeout(ctx, 2*time.Second)
	deadline, _ := native.Deadline()
	attempt, err := actions.Attempt(native, *work.Action, submit.Input.Authority, deadline.Truncate(time.Millisecond))
	stop()
	if err != nil {
		t.Fatal(err)
	}
	cancels := cancellationServiceFixture(t, db, intents, core)
	input := cancelInputFixture(ctx, submit, live.ClientOrderID, attempt.IdempotencyKey)
	input.Deadline, input.RequestID = attempt.Deadline, attempt.Plan.Key
	pending, err := cancels.Cancel(ctx, input, submit.ExecutionAuthority)
	if err != nil || pending.Request.State != domain.CancellationPending {
		t.Fatal(err)
	}
	if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil {
		t.Fatal(err)
	}
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, live.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil {
		t.Fatal(err)
	}
	assertAdminCancellationPending(ctx, t, control, actor, receipt)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	if _, err := cancels.Recover(ctx, submit.Input.AccountID, pending.Request.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Prepare(ctx, submit.Input.Authority, 0); err != nil {
		t.Fatal(err)
	}
	done, err := control.Receipt(ctx, actor.Token, receipt.StrategyID, receipt.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertAdminCancellationTerminal(t, done, live.Revision)
	if replay, err := control.Request(ctx, actor.Token, actor.CSRF, request); err != nil || replay != done {
		t.Fatal("same key lost the original evolving receipt", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "20")
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
	assertGatewayOrderCount(ctx, t, db, 2)
}

func assertAdminCancellationTerminal(t *testing.T, done domain.AdminCancellationReceipt, previousRevision int64) {
	t.Helper()
	if done.State != domain.AdminCancellationSucceeded || done.Outcome != domain.AdminCancellationTerminal || done.CompletedAt.IsZero() || done.ObservedOrderState != domain.OrderStateCanceled || done.ObservedOrderRevision <= previousRevision {
		t.Fatal("OMS terminal proof did not finish receipt")
	}
}

func adminCancelRequest(order domain.IntentSnapshot) domain.AdminCancellationRequest {
	return domain.AdminCancellationRequest{
		Key:                   "admin-order-cancel-request-01",
		StrategyID:            order.Input.Authority.StrategyID,
		OrderID:               order.ID,
		ExpectedOrderRevision: order.Revision,
		Reason:                "cancel the selected order only",
	}
}

func assertAdminCancellationPending(ctx context.Context, t *testing.T, control domain.AdminOrderCancellations, actor domain.AdminLogin, receipt domain.AdminCancellationReceipt) {
	t.Helper()
	read, err := control.Receipt(ctx, actor.Token, receipt.StrategyID, receipt.ID)
	if err != nil || read.State != domain.AdminCancellationAccepted || !read.CompletedAt.IsZero() || read.ObservedOrderRevision != 0 {
		t.Fatal("acceptance or core-only evidence falsely completed cancellation", err)
	}
}

func TestAdminCancellationValidatesCurrentAuthorityReplayAndOrderRevision(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	control, err := repository.NewAdminOrderCancellations(db)
	if err != nil {
		t.Fatal(err)
	}
	viewer := riskActorFixture(ctx, t, db, "cancel-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	request := adminCancelRequest(order)
	if _, err := control.Request(ctx, viewer.Token, viewer.CSRF, request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("viewer requested cancellation", err)
	}
	if _, err := control.Request(ctx, actor.Token, "invalid-csrf", request); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("cancellation omitted current CSRF", err)
	}
	stale := request
	stale.ExpectedOrderRevision++
	if _, err := control.Request(ctx, actor.Token, actor.CSRF, stale); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("stale order confirmation accepted", err)
	}
	receipt, err := control.Request(ctx, actor.Token, actor.CSRF, request)
	if err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Reason = "different content"
	if _, err := control.Request(ctx, actor.Token, actor.CSRF, changed); !errors.Is(err, domain.ErrAdminConflict) {
		t.Fatal("same management key was rebound", err)
	}
	if read, err := control.Receipt(ctx, viewer.Token, receipt.StrategyID, receipt.ID); err != nil || read != receipt {
		t.Fatal("viewer could not read immutable receipt", err)
	}
	if _, err := control.Receipt(ctx, viewer.Token, receipt.StrategyID+1, receipt.ID); !errors.Is(err, domain.ErrAdminNotFound) {
		t.Fatal("receipt read ignored strategy scope", err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Request(ctx, actor.Token, actor.CSRF, request); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("revoked actor replayed cancellation", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminAudit{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}
