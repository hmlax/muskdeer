//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyActionSurvivesUnknownCommitAndOwnerReplacementWithoutResubmitting(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	actions, err := repository.NewStrategyActions(db)
	if err != nil {
		t.Fatal(err)
	}
	plan := domain.StrategyActionPlan{
		Key:          "cycle-1-buy",
		Kind:         domain.StrategyActionSubmit,
		AccountID:    submit.Input.AccountID,
		InstrumentID: submit.Input.InstrumentID,
		Side:         domain.SideBuy,
		Price:        decimal.NewFromInt(99),
		Quantity:     decimal.NewFromInt(1),
		TimeInForce:  domain.TimeInForcePostOnly,
	}
	planned, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil || planned.State != domain.StrategyActionPending || planned.ID <= 0 || planned.IdempotencyKey == "" {
		t.Fatalf("action was not durable before dispatch: %+v, %v", planned, err)
	}
	repeated, err := actions.Plan(ctx, submit.Input.Authority, plan)
	if err != nil || repeated.ID != planned.ID || repeated.IdempotencyKey != planned.IdempotencyKey {
		t.Fatal("repeated planning changed its durable command identity")
	}
	changed := plan
	changed.Quantity = decimal.NewFromInt(2)
	if _, err := actions.Plan(ctx, submit.Input.Authority, changed); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatal("planning key was rebound to different business content")
	}
	attempted, err := actions.Attempt(ctx, planned, submit.Input.Authority, time.Now().Add(2*time.Second))
	if err != nil || attempted.State != domain.StrategyActionDispatching || attempted.Revision <= planned.Revision {
		t.Fatalf("dispatch was not recorded before I/O: %+v, %v", attempted, err)
	}
	if _, err := actions.Attempt(ctx, attempted, submit.Input.Authority, time.Now().Add(time.Second)); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("an attempted operation became eligible for another dispatch")
	}
	if _, err := actions.Discard(ctx, attempted, submit.Input.Authority); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("discard erased an uncertain operation")
	}
	unknown, err := actions.Observe(ctx, attempted, submit.Input.Authority)
	if err != nil || unknown.State != domain.StrategyActionUnknown {
		t.Fatalf("missing remote record was incorrectly treated as no effect: %+v, %v", unknown, err)
	}
	other := plan
	other.Key = "cycle-2-buy"
	if _, err := actions.Plan(ctx, submit.Input.Authority, other); !errors.Is(err, domain.ErrOutcomeUnknown) {
		t.Fatal("unresolved submission permitted a new business key")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	// Model the original in-flight request completing after an early missing
	// observation. There is exactly one actual submission, before lease loss.
	core, intents := executionFixture(t, db)
	input := submit.Input
	input.IdempotencyKey = planned.IdempotencyKey
	input.Price, input.Quantity = plan.Price, plan.Quantity
	input.Deadline = attempted.Deadline
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, input, submit.ExecutionAuthority)
	if err != nil || live.State != domain.OrderStateLive {
		t.Fatalf("original in-flight request failed: %+v, %v", live, err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: input.Authority.StrategyID,
	}
	expireLeaseFixture(ctx, t, db, key)
	if _, err := actions.Observe(ctx, unknown, input.Authority); !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("former owner changed action state after losing authority")
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "replacement-strategy")
	if err != nil {
		t.Fatal(err)
	}
	token, err := leases.Acquire(ctx, key, owner, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	authority := input.Authority
	authority.OwnerID, authority.OwnerEpoch = token.OwnerID, token.Epoch
	recovered, err := actions.Observe(ctx, unknown, authority)
	if err != nil || recovered.State != domain.StrategyActionObserved || recovered.IntentID != live.ID || recovered.IdempotencyKey != planned.IdempotencyKey {
		t.Fatalf("new owner did not observe the original persisted identity: %+v, %v", recovered, err)
	}
	assertGatewayOrderCount(ctx, t, db, 1)
	assertReservedBudget(ctx, t, db, input.AccountID, "99")
}
