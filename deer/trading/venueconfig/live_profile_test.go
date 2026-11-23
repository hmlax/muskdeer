package venueconfig

import (
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestLiveAccountProfileUpdatesLimitsAndLeverageWithoutMutatingStartup(t *testing.T) {
	budget := profileBudgetFixture()
	startup, err := ProfileFromBudget(budget)
	if err != nil {
		t.Fatal(err)
	}
	startup.StrategyInstanceId = 213
	original := proto.CloneOf(startup)
	budget.MaximumNotional, budget.MaximumOrderNotional, budget.Leverage = "4200", "50", 5
	current, err := ProfileWithPolicy(startup, budget)
	if err != nil || current.GetMaximumNotional() != "4200" || current.GetMaximumOrderNotional() != "50" || current.GetLeverage() != 5 || current.GetStrategyInstanceId() != 213 {
		t.Fatalf("account policy was not applied: %v %v", current, err)
	}
	if !proto.Equal(startup, original) {
		t.Fatal("live policy mutated shared startup snapshot")
	}
	budget.Symbol = "ETHUSDT"
	if _, err := ProfileWithPolicy(startup, budget); err == nil {
		t.Fatal("account identity changed under a running instance")
	}
}
