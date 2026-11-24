//go:build integration

package venue

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	orders "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestAccountDisableWaitsForFundWriterAndBlocksNewQuotesWithoutCancelling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	auditBefore := readAccountDisableAudit(ctx, t, setup)
	var before model.TradingAccountPolicy
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Take(&before).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := venueconfig.ProfileFromBudget(&before)
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{}, 1)
	setup.runtime.gates = map[string]chan struct{}{before.AccountId: gate}
	parent := &Instances{
		ordered: []*Runtime{setup.runtime},
		store:   setup.store,
	}
	request := &pb.AccountPolicyDisable{
		Id:                  before.Id,
		ExpectedUpdatedAtMs: before.UpdateAt,
		Scope:               native.Scope(profile),
	}
	if _, err := parent.DisableAccountPolicy(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("untrusted disable admitted: %v", err)
	}
	trusted := washTestContext(ctx)
	snapshot := proto.CloneOf(setup.snapshot)
	snapshot.Positions = nil
	snapshot.Orders = nil
	snapshot.Available = "10000"
	snapshot.Market.WorstFeeRate = "0"
	cycle := &quoteCycle{snapshot: snapshot}
	quote := gateway.QuoteRequest{
		Scope: gateway.Scope{
			UserID: before.UserId,
			FundID: before.FundId,
			Symbol: before.Symbol,
		},
		Price:    decimal.NewFromInt(1),
		Quantity: decimal.NewFromInt(1),
		Leverage: before.Leverage,
	}
	if err := setup.runtime.checkDirectQuoteBudget(ctx, cycle, quote); err != nil {
		t.Fatalf("valid enabled quote was not admissible: %v", err)
	}

	gate <- struct{}{}
	bounded, stop := context.WithTimeout(trusted, 10*time.Millisecond)
	_, err = parent.DisableAccountPolicy(bounded, request)
	stop()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("disable bypassed in-flight writer: %v", err)
	}
	<-gate
	result, err := parent.DisableAccountPolicy(trusted, request)
	if err != nil || result.GetEnabled() || result.GetUpdatedAtMs() <= before.UpdateAt {
		t.Fatalf("disable failed: %v %v", result, err)
	}
	current := &model.TradingAccountPolicy{}
	if err := setup.db.WithContext(ctx).Model(current).Where("id = ?", before.Id).Take(current).Error; err != nil {
		t.Fatal(err)
	}
	expected := proto.CloneOf(&before)
	expected.Enabled = false
	expected.UpdateAt = result.UpdatedAtMs
	if !proto.Equal(current, expected) {
		t.Fatal("disable changed limits or identity")
	}
	budget, err := setup.store.Budget(ctx, before.AccountId)
	if err != nil || budget.Enabled {
		t.Fatalf("current quote admission still sees enabled: %v", err)
	}
	if _, err := parent.DisableAccountPolicy(trusted, request); status.Code(err) != codes.Aborted {
		t.Fatal("stale disable accepted")
	}
	request.ExpectedUpdatedAtMs = result.UpdatedAtMs
	repeated, err := parent.DisableAccountPolicy(trusted, request)
	if err != nil || repeated.GetUpdatedAtMs() != result.UpdatedAtMs {
		t.Fatal("no-op disable changed version")
	}
	if err := setup.runtime.checkDirectQuoteBudget(ctx, cycle, quote); err != ErrRiskLimit {
		t.Fatalf("disabled account admitted the same previously valid quote: %v", err)
	}
	verifyAccountDisableAudit(ctx, t, setup, auditBefore)
}

type accountDisableAudit struct {
	intent        orders.VenueOrderIntent
	instance      strategy.StrategyInstance
	intents       int64
	cancellations int64
}

func readAccountDisableAudit(ctx context.Context, t *testing.T, setup *currentStateTest) *accountDisableAudit {
	t.Helper()
	facts := &accountDisableAudit{}
	if err := setup.db.WithContext(ctx).Model(&orders.VenueOrderIntent{}).Where("id = ?", setup.old.Id).Take(&facts.intent).Error; err != nil {
		t.Fatal(err)
	}
	if err := setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", setup.instance.Id).Take(&facts.instance).Error; err != nil {
		t.Fatal(err)
	}
	if err := setup.db.WithContext(ctx).Model(&orders.VenueOrderIntent{}).Count(&facts.intents).Error; err != nil {
		t.Fatal(err)
	}
	if err := setup.db.WithContext(ctx).Model(&orders.VenueCancellation{}).Count(&facts.cancellations).Error; err != nil {
		t.Fatal(err)
	}
	return facts
}
func verifyAccountDisableAudit(ctx context.Context, t *testing.T, setup *currentStateTest, before *accountDisableAudit) {
	t.Helper()
	after := readAccountDisableAudit(ctx, t, setup)
	if !proto.Equal(&before.intent, &after.intent) || !proto.Equal(&before.instance, &after.instance) || before.intents != after.intents || before.cancellations != after.cancellations {
		t.Fatal("disable rewrote history or instance state")
	}
	if setup.client.cancellations.Load() != 0 || setup.client.submissions.Load() != 0 {
		t.Fatal("disable invoked SDK order operations")
	}
}
