package venueconfig

import (
	"encoding/hex"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonmodelv1 "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func profileBudgetFixture() *commonmodelv1.TradingAccountPolicy {
	return &commonmodelv1.TradingAccountPolicy{
		AccountId:            native.ScopeKey(101, 201),
		UserId:               101,
		FundId:               201,
		Symbol:               "BTCUSDT",
		MaximumNotional:      "1000.000000000000000000000000000000000000",
		MaximumOrderNotional: "100.000000000000000000000000000000000000",
		Leverage:             10,
		MarketDataSource:     native.Source,
		PriceTick:            "0.100000000000000000000000000000000000",
		QuantityStep:         "0.001000000000000000000000000000000000",
	}
}

func TestDatabaseProfileRejectsInvalidBusinessPolicy(t *testing.T) {
	row := profileBudgetFixture()
	if p, err := ProfileFromBudget(row); err != nil || p.GetPriceTick() != "0.1" || p.GetMaximumNotional() != "1000" {
		t.Fatalf("database numeric normalization: %v, %v", p, err)
	}
	for _, mutate := range []func(*commonmodelv1.TradingAccountPolicy){
		func(p *commonmodelv1.TradingAccountPolicy) { p.AccountId = native.ScopeKey(102, 201) },
		func(p *commonmodelv1.TradingAccountPolicy) { p.Leverage = 11 },
		func(p *commonmodelv1.TradingAccountPolicy) { p.PriceTick = "0" },
		func(p *commonmodelv1.TradingAccountPolicy) { p.QuantityStep = "0" },
		func(p *commonmodelv1.TradingAccountPolicy) { p.MarketDataSource = "okx" },
		func(p *commonmodelv1.TradingAccountPolicy) { p.MaximumNotional = "0" },
		func(p *commonmodelv1.TradingAccountPolicy) { p.MaximumOrderNotional = "1001" },
		func(p *commonmodelv1.TradingAccountPolicy) { p.DeleteAt = proto.Int64(1) },
	} {
		changed := proto.CloneOf(row)
		mutate(changed)
		if _, err := ProfileFromBudget(changed); err == nil {
			t.Fatal("unsafe database business profile accepted")
		}
	}
}

func TestDatabasePolicyKeepsLegacySyntheticFingerprint(t *testing.T) {
	p, err := ProfileFromBudget(profileBudgetFixture())
	if err != nil {
		t.Fatal(err)
	}
	policy := &commonv1.HimalayaPolicy{
		Target:           "127.0.0.1:8080",
		TokenEnv:         "TEST_TOKEN",
		Profiles:         []*commonv1.HimalayaProfilePolicy{p},
		QuantityFraction: "0.1",
		Depth:            20,
		LeaseTtl:         durationpb.New(10 * time.Second),
		SyntheticTape: &configv1.SyntheticTape{
			Enabled:         true,
			DevelopmentOnly: true,
			RunId:           "legacy-policy",
			Interval:        durationpb.New(time.Second),
			MaximumEvents:   2,
			Mode:            configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE,
			Profiles: []*configv1.SyntheticTapeProfile{{
				Symbol:          "BTCUSDT",
				Quantity:        "0.001",
				MaximumNotional: "200",
			}},
		},
	}
	// Captured from the prior generated config type, before removing profiles.
	const legacy = "0a0e3132372e302e302e313a38303830120a544553545f544f4b454e1a30086510c9011a07425443555344542204313030302a03313030300a3a0762696e616e63654203302e314a05302e3030312203302e3128143202080a3a32080110011a0d6c65676163792d706f6c69637922020801280232150a07425443555344541205302e3030311a033230303801"
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(policy)
	if err != nil || hex.EncodeToString(data) != legacy {
		t.Fatalf("database move changed retained synthetic run identity: %x, %v", data, err)
	}
}
