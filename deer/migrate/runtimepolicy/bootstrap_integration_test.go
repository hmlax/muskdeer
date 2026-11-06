//go:build integration

package runtimepolicy_test

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/corelocal"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	executionmodel "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodel "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
)

func TestRuntimePolicyBootstrapPreservesOperatorChangesAndUnfundedAccount(t *testing.T) {
	db, ctx := policyDatabase(t)
	resources, err := corelocal.Seed(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	input, err := corelocal.Policies(resources)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeconfig.SeedDatabasePolicies(ctx, db, input); err != nil {
		t.Fatal(err)
	}
	market := readPolicy(ctx, t, db, "market")
	if !market.Parameters.Enabled || len(market.Parameters.Market.Feeds) != 2 || market.Parameters.Market.Feeds[0].ReplayBid != "99.99" {
		t.Fatal("explicit bootstrap lost the database replay parameters")
	}
	changed := proto.CloneOf(readPolicy(ctx, t, db, "strategy").Parameters)
	changed.Enabled = false
	changed.PollInterval = durationpb.New(125 * time.Millisecond)
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []*configv1.RuntimePolicyInput{
		{
			ServiceName:      "strategy",
			ExpectedRevision: 1,
			Parameters:       changed,
		},
	}); err != nil {
		t.Fatal(err)
	}
	before := readPolicy(ctx, t, db, "strategy")
	if err := runtimeconfig.SeedDatabasePolicies(ctx, db, input); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(before, readPolicy(ctx, t, db, "strategy")) || !proto.Equal(market, readPolicy(ctx, t, db, "market")) {
		t.Fatal("repeated initialization replaced an operator policy")
	}
	var budget model.AccountBudget
	if err := db.WithContext(ctx).Model(&model.AccountBudget{}).Where("account_id = ?", corelocal.AccountID).Take(&budget).Error; err != nil {
		t.Fatal(err)
	}
	if budget.Enabled || budget.Revision != 1 {
		t.Fatal("policy initialization enabled or reset a trading budget")
	}
	for _, entity := range []any{&executionmodel.SimulatorBalance{}, &strategymodel.StrategyInstance{}, &executionmodel.OrderIntent{}, &executionmodel.SimulatorOrder{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("policy bootstrap created financial state: %d, %v", count, err)
		}
	}
}
