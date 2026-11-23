package venue

import (
	"testing"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"google.golang.org/protobuf/proto"
)

func accountPolicyFixture() (*model.TradingAccountPolicy, *pb.AccountPolicyUpdate, *common.VenueSnapshot) {
	policy := &model.TradingAccountPolicy{
		Id:                   1,
		UpdateAt:             42,
		AccountId:            native.ScopeKey(1, 1),
		UserId:               1,
		FundId:               1,
		Symbol:               "LINKUSDT",
		MarketDataSource:     native.Source,
		PriceTick:            "0.001",
		QuantityStep:         "0.01",
		MaximumNotional:      "4200",
		MaximumOrderNotional: "100",
		Leverage:             10,
		Enabled:              true,
	}
	scope := &common.VenueScope{
		UserId: 1,
		FundId: 1,
		Symbol: policy.Symbol,
	}
	request := &pb.AccountPolicyUpdate{
		Id:                   policy.Id,
		ExpectedUpdatedAtMs:  policy.UpdateAt,
		Scope:                scope,
		MaximumNotional:      "4200",
		MaximumOrderNotional: "50",
		Leverage:             5,
	}
	snapshot := &common.VenueSnapshot{
		Scope:        proto.CloneOf(scope),
		ValidUntilMs: time.Now().Add(5 * time.Second).UnixMilli(),
		Market: &common.VenueMarket{
			PriceTick:       policy.PriceTick,
			QuantityStep:    policy.QuantityStep,
			MaximumLeverage: 10,
			MarkPrice:       "10",
			BestAsk:         "10",
		},
	}
	return policy, request, snapshot
}

func TestAccountPolicyRejectsStaleForeignUnsafeInputsAndExistingPositionLeverageChange(t *testing.T) {
	policy, request, snapshot := accountPolicyFixture()
	if _, err := validateAccountPolicy(policy, request, snapshot, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pb.AccountPolicyUpdate, *common.VenueSnapshot){
		func(in *pb.AccountPolicyUpdate, s *common.VenueSnapshot) {
			in.MaximumNotional, in.Leverage = "99", policy.Leverage
			s.Orders = policyActiveOrders()
		},
		func(_ *pb.AccountPolicyUpdate, s *common.VenueSnapshot) { s.Orders = policyActiveOrders() },
		func(in *pb.AccountPolicyUpdate, _ *common.VenueSnapshot) { in.MaximumOrderNotional = "4201" },
		func(in *pb.AccountPolicyUpdate, _ *common.VenueSnapshot) { in.Leverage = 11 },
		func(in *pb.AccountPolicyUpdate, _ *common.VenueSnapshot) { in.ExpectedUpdatedAtMs-- },
		func(_ *pb.AccountPolicyUpdate, s *common.VenueSnapshot) { s.Scope.FundId++ },
		func(_ *pb.AccountPolicyUpdate, s *common.VenueSnapshot) { s.ValidUntilMs = 0 },
		func(_ *pb.AccountPolicyUpdate, s *common.VenueSnapshot) { s.Market.QuantityStep = "1" },
		func(_ *pb.AccountPolicyUpdate, s *common.VenueSnapshot) {
			s.Positions = []*common.VenuePosition{
				{
					Symbol:         s.Scope.Symbol,
					SignedQuantity: "1",
				},
			}
		},
	} {
		in, s := proto.CloneOf(request), proto.CloneOf(snapshot)
		mutate(in, s)
		if _, err := validateAccountPolicy(policy, in, s, time.Now().UnixMilli()); err == nil {
			t.Fatal("unsafe account update accepted")
		}
	}
}

func TestAccountPolicyKeepsExistingPositionWhenLeverageDoesNotChange(t *testing.T) {
	policy, request, snapshot := accountPolicyFixture()
	request.Leverage = policy.Leverage
	snapshot.Positions = []*common.VenuePosition{
		{
			Symbol:         policy.Symbol,
			SignedQuantity: "1",
		},
	}
	if _, err := validateAccountPolicy(policy, request, snapshot, time.Now().UnixMilli()); err != nil {
		t.Fatalf("unchanged leverage rejected an existing position: %v", err)
	}
}

func policyActiveOrders() []*common.VenueOrder {
	return []*common.VenueOrder{
		{
			Price:            "10",
			Quantity:         "10",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		},
	}
}
