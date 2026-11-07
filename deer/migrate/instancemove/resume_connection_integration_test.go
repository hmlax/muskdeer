//go:build integration

package instancemove

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	config "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	values "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestConsumedTapeRestartsWithTheExecutionConnectionWithoutReset(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	source, target := legacy(ctx, t), database(t)
	if err := Move(ctx, source, target); err != nil {
		t.Fatal(err)
	}
	instances, err := instanceconfig.Load(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	var wash *instanceconfig.Instance
	for _, instance := range instances {
		if instance.Type == domain.StrategyTemplateWashTrade {
			wash = instance
		}
	}
	if wash == nil || wash.TapePolicyDigest != "" {
		t.Fatal("fresh finite run required")
	}
	connection := &config.VenueConnection{
		Target:   "127.0.0.1:8080",
		TokenEnv: "TEST_VENUE_TOKEN",
	}
	settings, err := wash.Settings()
	if err != nil {
		t.Fatal(err)
	}
	// Match NewInstances: these transport fields are attached before the SDK
	// publisher computes its durable policy identity.
	settings.Venue.Target, settings.Venue.TokenEnv = connection.Target, connection.TokenEnv
	digest, err := native.SyntheticPolicyDigest(settings.Venue)
	if err != nil {
		t.Fatal(err)
	}
	for sequence := range 3 {
		if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Create(&execution.SyntheticTapeIntent{
			RunId:        wash.Parameters.WashTrade.RunId,
			Symbol:       wash.Profile.Symbol,
			Sequence:     int64(sequence),
			PolicyDigest: digest,
			Plan: &values.SyntheticTapePlan{
				RunId:    wash.Parameters.WashTrade.RunId,
				Symbol:   wash.Profile.Symbol,
				Sequence: uint32(sequence),
			},
			Notional: "1",
			Result:   &values.SyntheticTapeResult{State: native.SyntheticUnknown},
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var before []*execution.SyntheticTapeIntent
	if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	for restart := range 3 {
		loaded, err := instanceconfig.Load(ctx, target, connection)
		if err != nil || len(loaded) != 2 {
			t.Fatalf("Load(restart %d, execution connection) = %d, %v, want both instances", restart, len(loaded), err)
		}
		for _, instance := range loaded {
			if instance.Type == domain.StrategyTemplateWashTrade && instance.ResumeSequence != 3 {
				t.Fatalf("Load(restart %d) resume = %d, want 3 consumed unknown slots", restart, instance.ResumeSequence)
			}
		}
	}
	wrong := proto.CloneOf(connection)
	wrong.Target = "127.0.0.1:8081"
	if _, err := instanceconfig.Load(ctx, target, wrong); err == nil {
		t.Fatal("Load(changed execution identity) accepted incompatible consumed slots")
	}
	input := &values.StrategyInstanceImport{Instances: []*values.StrategyInstanceInput{{
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
	var versionsBefore int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&versionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := instanceconfig.Import(ctx, target, input, connection); err != nil {
		t.Fatalf("Import(unchanged consumed run, execution connection) = %v", err)
	}
	input.Instances[0].Parameters.WashTrade.Quantity = "0.02"
	if err := instanceconfig.Import(ctx, target, input, connection); err == nil {
		t.Fatal("Import(changed parameters, same consumed run) accepted")
	}
	var versionsAfter int64
	if err := target.WithContext(ctx).Model(&strategy.StrategyConfigVersion{}).Count(&versionsAfter).Error; err != nil || versionsAfter != versionsBefore {
		t.Fatalf("unchanged/failed imports appended audit: %d -> %d, %v", versionsBefore, versionsAfter, err)
	}
	var after []*execution.SyntheticTapeIntent
	if err := target.WithContext(ctx).Model(&execution.SyntheticTapeIntent{}).Order("id").Find(&after).Error; err != nil || len(after) != len(before) {
		t.Fatalf("consumed slots changed: %d -> %d, %v", len(before), len(after), err)
	}
	for index := range before {
		if !proto.Equal(before[index], after[index]) {
			t.Fatal("restart or failed maintenance changed a consumed slot")
		}
	}
}
