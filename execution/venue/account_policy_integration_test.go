//go:build integration

package venue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodel "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type policyObservationClient struct {
	gateway.Client
	gateway.LongGridClient
	snapshot *common.VenueSnapshot
}

func (c *policyObservationClient) ReadGrid(context.Context, gateway.Scope) (*gateway.Observation, error) {
	snapshot := proto.CloneOf(c.snapshot)
	snapshot.ValidUntilMs = time.Now().Add(time.Second).UnixMilli()
	return &gateway.Observation{Snapshot: snapshot}, nil
}

func TestAccountPolicyCASUpdatesAllLiveParametersAndPreservesHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	var policyBefore model.TradingAccountPolicy
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Take(&policyBefore).Error; err != nil {
		t.Fatal(err)
	}
	var auditBefore executionmodel.VenueOrderIntent
	if err := setup.db.WithContext(ctx).Model(&executionmodel.VenueOrderIntent{}).Where("id = ?", setup.old.Id).Take(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	var instanceBefore strategy.StrategyInstance
	if err := setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", setup.instance.Id).Take(&instanceBefore).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := proto.CloneOf(setup.snapshot)
	snapshot.Positions = nil
	snapshot.Orders = nil
	client := &policyObservationClient{snapshot: snapshot}
	startup, err := venueconfig.ProfileFromBudget(setup.budget)
	if err != nil {
		t.Fatal(err)
	}
	startup.StrategyInstanceId = setup.instance.Id
	gate := make(chan struct{}, 1)
	setup.runtime.gates = map[string]chan struct{}{setup.budget.AccountId: gate}
	parent := &Instances{
		ordered: []*Runtime{setup.runtime},
		store:   setup.store,
		client:  &atomic.Pointer[gatewaySession]{},
		closing: &atomic.Bool{},
	}
	parent.client.Store(&gatewaySession{Client: client})
	request := &pb.AccountPolicyUpdate{
		Id:                   setup.budget.Id,
		ExpectedUpdatedAtMs:  setup.budget.UpdateAt,
		Scope:                native.Scope(startup),
		MaximumNotional:      "4200",
		MaximumOrderNotional: "50",
		Leverage:             5,
	}
	trusted := washTestContext(ctx)
	if _, err := parent.UpdateAccountPolicy(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("untrusted write accepted: %v", err)
	}
	gate <- struct{}{}
	blocked, cancelBlocked := context.WithTimeout(trusted, 10*time.Millisecond)
	_, err = parent.UpdateAccountPolicy(blocked, request)
	cancelBlocked()
	if status.Code(err) != codes.DeadlineExceeded {
		t.Fatalf("policy change bypassed in-flight fund gate: %v", err)
	}
	<-gate
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, err := parent.UpdateAccountPolicy(trusted, request); results <- err })
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch status.Code(err) {
		case codes.OK:
			successes++
		case codes.Aborted:
			conflicts++
		default:
			t.Fatalf("unexpected update outcome: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("stale concurrent writes accepted: %d/%d", successes, conflicts)
	}
	current, err := venueconfig.CurrentProfile(ctx, setup.db, startup)
	if err != nil || current.GetMaximumNotional() != "4200" || current.GetMaximumOrderNotional() != "50" || current.GetLeverage() != 5 {
		t.Fatalf("live worker still uses startup policy: %v %v", current, err)
	}
	verifyLiveAccountQuote(trusted, t, setup, current)
	if startup.MaximumNotional != "1000" || startup.MaximumOrderNotional != "100" || startup.Leverage != 10 {
		t.Fatal("startup profile mutated")
	}
	var saved model.TradingAccountPolicy
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", request.Id).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	expected := proto.CloneOf(&policyBefore)
	expected.MaximumNotional, expected.MaximumOrderNotional, expected.Leverage, expected.UpdateAt = saved.MaximumNotional, saved.MaximumOrderNotional, 5, saved.UpdateAt
	if saved.UpdateAt <= request.ExpectedUpdatedAtMs || !proto.Equal(expected, &saved) {
		t.Fatal("account update touched unrelated policy fields")
	}
	request.ExpectedUpdatedAtMs = saved.UpdateAt
	reply, err := parent.UpdateAccountPolicy(trusted, request)
	if err != nil || reply.GetUpdatedAtMs() != saved.UpdateAt {
		t.Fatalf("same-value save changed version: %v %v", reply, err)
	}
	var instance strategy.StrategyInstance
	if err := setup.db.WithContext(ctx).Model(&strategy.StrategyInstance{}).Where("id = ?", setup.instance.Id).Take(&instance).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&instanceBefore, &instance) {
		t.Fatal("account update rewrote instance control or parameter history")
	}
	var old executionmodel.VenueOrderIntent
	if err := setup.db.WithContext(ctx).Model(&executionmodel.VenueOrderIntent{}).Where("id = ?", setup.old.Id).Take(&old).Error; err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(&auditBefore, &old) {
		t.Fatal("account update changed historical UNKNOWN")
	}
}

func verifyLiveAccountQuote(ctx context.Context, t *testing.T, setup *currentStateTest, current *common.VenueProfilePolicy) {
	t.Helper()
	snapshot := proto.CloneOf(setup.snapshot)
	snapshot.Positions, snapshot.Orders = nil, nil
	setup.runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	cycle := &quoteCycle{
		profile:  current,
		snapshot: snapshot,
		authority: LiveAuthority{
			InstanceID: setup.instance.Id,
			Revision:   setup.instance.Revision,
		},
	}
	quote, err := setup.runtime.prepareDirectQuote(ctx, cycle, &common.VenueTarget{
		Side:     string(domain.SideBuy),
		Price:    "10",
		Quantity: "1",
	})
	if err != nil || quote.Leverage != 5 {
		t.Fatalf("next SDK request did not use the new leverage: %v %v", quote, err)
	}
	if _, err := setup.runtime.prepareDirectQuote(ctx, cycle, &common.VenueTarget{
		Side:     string(domain.SideBuy),
		Price:    "10",
		Quantity: "10",
	}); !errors.Is(err, ErrRiskLimit) {
		t.Fatalf("new single-order cap was not applied: %v", err)
	}
}

func TestAccountPolicyCannotInvalidatePausedGridConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	params := &common.StrategyInstanceParameters{
		SchemaVersion: 1,
		Execution:     proto.CloneOf(setup.instance.Parameters.Execution),
		LongGrid: &common.LongGridParameters{
			SpacingFraction:         "0.01",
			StopLossFraction:        "0.005",
			OrderNotional:           "100",
			MaximumPositionNotional: "100",
			OpenOnStart:             true,
			HaltAfterStopLoss:       true,
		},
	}
	grid, err := instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation: instanceconfig.ManagementCreate,
		Subject:   "account-policy-test",
		RequestID: "account-policy-grid-create-regression",
		Input: &common.StrategyInstanceInput{
			InstanceKey:      "policy-grid",
			TemplateId:       strategyregistry.LongGridTemplateID,
			TemplateRevision: 1,
			AccountId:        setup.budget.AccountId,
			InstrumentId:     setup.instance.InstrumentId,
			MarketDataSource: native.Source,
			Parameters:       params,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := proto.CloneOf(setup.snapshot)
	snapshot.Positions, snapshot.Orders = nil, nil
	parent := &Instances{
		store:   setup.store,
		client:  &atomic.Pointer[gatewaySession]{},
		closing: &atomic.Bool{},
	}
	parent.client.Store(&gatewaySession{Client: &policyObservationClient{snapshot: snapshot}})
	_, err = parent.UpdateAccountPolicy(washTestContext(ctx), &pb.AccountPolicyUpdate{
		Id:                   setup.budget.Id,
		ExpectedUpdatedAtMs:  setup.budget.UpdateAt,
		Scope:                snapshot.Scope,
		MaximumNotional:      "1000",
		MaximumOrderNotional: "50",
		Leverage:             10,
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("policy invalidated paused grid startup: %v", err)
	}
	changed := proto.CloneOf(params)
	changed.LongGrid.OrderNotional, changed.LongGrid.MaximumPositionNotional = "101", "150"
	_, err = instanceconfig.ManageConfigured(ctx, setup.db, instanceconfig.ManagementWrite{
		Operation:              instanceconfig.ManagementUpdate,
		Subject:                "account-policy-test",
		RequestID:              "account-policy-grid-edit-regression",
		ID:                     grid.Id,
		ExpectedRevision:       grid.Revision,
		ExpectedControlVersion: grid.RequestedConfigVersion,
		Input:                  &common.StrategyInstanceInput{Parameters: changed},
	})
	if !errors.Is(err, instanceconfig.ErrInvalid) {
		t.Fatalf("strategy parameters exceeded locked account limits: %v", err)
	}
}
