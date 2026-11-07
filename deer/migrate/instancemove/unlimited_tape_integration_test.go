//go:build integration

package instancemove

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestConsumedTapeCanRemoveOnlyQuotasWithoutReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source, target := legacy(ctx, t), database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	connection := &config.VenueConnection{
		Target:   "127.0.0.1:8080",
		TokenEnv: "TEST_VENUE_TOKEN",
	}
	instances, err := instanceconfig.Load(ctx, target, connection)
	if err != nil {
		t.Fatal(err)
	}
	var wash *instanceconfig.Instance
	for _, instance := range instances {
		if instance.Type == domain.StrategyTemplateWashTrade {
			wash = instance
		}
	}
	input := &common.StrategyInstanceImport{Instances: []*common.StrategyInstanceInput{{
		InstanceKey:      wash.Key,
		TemplateId:       wash.TemplateID,
		TemplateRevision: wash.TemplateRevision,
		AccountId:        native.ScopeKey(wash.Profile.UserId, wash.Profile.FundId),
		InstrumentId:     1,
		MarketDataSource: string(wash.Source),
		DesiredRunning:   true,
		Parameters:       proto.CloneOf(wash.Parameters),
		ExpectedRevision: wash.Revision,
	}}}
	input.Instances[0].Parameters.WashTrade.MaximumEvents = 1000
	if err := instanceconfig.Import(ctx, target, input, connection); err != nil {
		t.Fatal(err)
	}
	wash.Parameters = proto.CloneOf(input.Instances[0].Parameters)
	settings, err := wash.Settings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Venue.Target, settings.Venue.TokenEnv = connection.Target, connection.TokenEnv
	digest, err := native.SyntheticPolicyDigest(settings.Venue)
	if err != nil {
		t.Fatal(err)
	}
	before := make([]*execution.SyntheticTapeIntent, 0, 1000)
	for sequence := range 1000 {
		before = append(before, &execution.SyntheticTapeIntent{
			RunId:        wash.Parameters.WashTrade.RunId,
			Symbol:       wash.Profile.Symbol,
			Sequence:     int64(sequence),
			PolicyDigest: digest,
			Plan: &common.SyntheticTapePlan{
				RunId:    wash.Parameters.WashTrade.RunId,
				Symbol:   wash.Profile.Symbol,
				Sequence: uint64(sequence),
			},
			Notional: "1",
			Result:   &common.SyntheticTapeResult{State: native.SyntheticUnknown},
		})
	}
	if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).CreateInBatches(before, 100).Error; err != nil {
		t.Fatal(err)
	}
	var storedBefore []*execution.SyntheticTapeIntent
	if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Order("sequence").Find(&storedBefore).Error; err != nil {
		t.Fatal(err)
	}
	before = storedBefore
	input.Instances[0].ExpectedRevision++
	input.Instances[0].Parameters.WashTrade.MaximumEvents = 0
	input.Instances[0].Parameters.WashTrade.MaximumNotional = "0"
	if err := instanceconfig.Import(ctx, target, input, connection); err != nil {
		t.Fatalf("Import(remove quotas on consumed run) = %v, want same run continued", err)
	}
	for restart := range 3 {
		loaded, err := instanceconfig.Load(ctx, target, connection)
		if err != nil {
			t.Fatal(err)
		}
		for _, instance := range loaded {
			if instance.Type == domain.StrategyTemplateWashTrade && (instance.ResumeSequence != 1000 || instance.TapePolicyDigest != digest || instance.Parameters.WashTrade.RunId != wash.Parameters.WashTrade.RunId) {
				t.Fatalf("Load(restart %d) lost consumed cursor or audit identity: %v", restart, instance)
			}
		}
	}
	var versions int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&versions).Error; err != nil {
		t.Fatal(err)
	}
	if err := instanceconfig.Import(ctx, target, input, connection); err != nil {
		t.Fatal(err)
	}
	input.Instances[0].ExpectedRevision++
	input.Instances[0].Parameters.WashTrade.Quantity = "0.02"
	if err := instanceconfig.Import(ctx, target, input, connection); err == nil {
		t.Fatal("Import(quantity mutation on consumed unlimited run) accepted")
	}
	var afterVersions int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&afterVersions).Error; err != nil || afterVersions != versions {
		t.Fatalf("repeated/failed maintenance appended history: %d -> %d, err=%v", versions, afterVersions, err)
	}
	var after []*execution.SyntheticTapeIntent
	if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Order("sequence").Find(&after).Error; err != nil || len(after) != len(before) {
		t.Fatalf("consumed audit count = %d, want %d, err=%v", len(after), len(before), err)
	}
	for i := range before {
		if !proto.Equal(before[i], after[i]) {
			t.Fatalf("quota removal rewrote consumed slot %d", i)
		}
	}
}
