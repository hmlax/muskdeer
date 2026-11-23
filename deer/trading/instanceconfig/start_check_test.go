package instanceconfig

import (
	"errors"
	"testing"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"google.golang.org/protobuf/proto"
)

func TestStartCheckPinsFinancialPolicyAndFreshness(t *testing.T) {
	budget := &model.TradingAccountPolicy{
		AccountId:            native.ScopeKey(1, 1),
		UserId:               1,
		FundId:               1,
		Symbol:               "LINKUSDT",
		MarketDataSource:     native.Source,
		MaximumNotional:      "4200",
		MaximumOrderNotional: "100",
		PriceTick:            "0.001",
		QuantityStep:         "0.01",
		Leverage:             10,
		Enabled:              true,
		UpdateAt:             42,
	}
	profile, err := venueconfig.ProfileFromBudget(budget)
	if err != nil {
		t.Fatal(err)
	}
	profile.StrategyInstanceId = 213
	now := time.Now().UnixMilli()
	proof := &StartCheck{
		Profile:          profile,
		PolicyUpdatedAt:  budget.UpdateAt,
		RequiredNotional: "3998.12383",
		ValidUntilMs:     now + 5000,
	}
	if err := validateStartCheck(budget, proof, now); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*model.TradingAccountPolicy){
		func(b *model.TradingAccountPolicy) { b.MaximumNotional = "100" },
		func(b *model.TradingAccountPolicy) { b.MaximumOrderNotional = "50" },
		func(b *model.TradingAccountPolicy) { b.UpdateAt++ },
		func(b *model.TradingAccountPolicy) { b.FundId++ },
	} {
		changed := proto.CloneOf(budget)
		change(changed)
		if err := validateStartCheck(changed, proof, now); !errors.Is(err, ErrConflict) {
			t.Fatalf("changed policy accepted: %v", err)
		}
	}
	if err := validateStartCheck(budget, proof, proof.ValidUntilMs); !errors.Is(err, ErrStartCheckExpired) {
		t.Fatalf("expired check accepted: %v", err)
	}
	proof.RequiredNotional = "4200.000000000000000000000000000000000001"
	if err := validateStartCheck(budget, proof, now); !errors.Is(err, ErrMakerBudget) {
		t.Fatalf("over-budget check accepted: %v", err)
	}
	proof.Profile = &common.VenueProfilePolicy{}
	if err := validateStartCheck(budget, proof, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("unbound proof accepted: %v", err)
	}
}
