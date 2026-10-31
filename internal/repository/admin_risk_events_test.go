package repository

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminRiskEventValuesPreserveRawExpiredDiagnosticsAndExactIdentity(t *testing.T) {
	row := &modelv1.RiskSnapshotEvent{
		Id:                   9007199254740993,
		CreateAt:             1,
		UpdateAt:             1,
		RiskSnapshotId:       9007199254740994,
		RiskSnapshotRevision: 9007199254740995,
		Kind:                 string(domain.RiskSnapshotEventChanged),
		StrategyInstanceId:   9007199254740996,
		AccountId:            "historical.account:one",
		InstrumentId:         9007199254740997,
		ConfigVersion:        9007199254740998,
		MarketDataSource:     string(domain.MarketSourceOKX),
		MarketFeedId:         proto.Int64(9007199254740999),
		MarketEpoch:          9007199254741000,
		MarketSequence:       9007199254741001,
		MarketRevision:       9007199254741002,
		MarketSourceVersion:  9007199254741003,
		MarketMaximumAgeMs:   5000,
		ObservedAt:           1000,
		ExpiresAt:            2000,
		Reconciled:           false,
		AllowNewRisk:         true,
		ReasonCode:           string(domain.RiskReasonReady),
	}
	value, err := adminRiskEventValue(row)
	if err != nil || value.ID != row.Id || value.SnapshotRevision != row.RiskSnapshotRevision || value.MarketSequence != row.MarketSequence ||
		value.PublisherEpoch != row.MarketEpoch || value.RecordedAt.UnixMilli() != 1 || value.ObservedAt.UnixMilli() != 1000 || value.ExpiresAt.UnixMilli() != 2000 ||
		value.RecordedReconciled || !value.RecordedAllowNewRisk || value.Reason != domain.RiskReasonReady || value.FeedID == nil || *value.FeedID != *row.MarketFeedId {
		t.Fatal("historical values were rounded, refreshed, reinterpreted or rejected as currently stale", err)
	}
	*value.FeedID = 1
	if *row.MarketFeedId != 9007199254740999 {
		t.Fatal("read DTO aliases a generated source pointer")
	}
	for _, change := range []func(*modelv1.RiskSnapshotEvent){
		func(r *modelv1.RiskSnapshotEvent) { r.Id = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.CreateAt = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.UpdateAt++ },
		func(r *modelv1.RiskSnapshotEvent) { r.DeleteAt = proto.Int64(1) },
		func(r *modelv1.RiskSnapshotEvent) { r.RiskSnapshotId = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.RiskSnapshotRevision = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.StrategyInstanceId = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.AccountId = "ambiguous%account" },
		func(r *modelv1.RiskSnapshotEvent) { r.InstrumentId = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.ConfigVersion = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.Kind = "undefined" },
		func(r *modelv1.RiskSnapshotEvent) { r.Kind = string(domain.RiskSnapshotEventRemoved) },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketDataSource = string(domain.MarketSourceBitget) },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketFeedId = proto.Int64(0) },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketEpoch = -1 },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketSequence = -1 },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketRevision = -1 },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketSourceVersion = -1 },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketMaximumAgeMs = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.MarketMaximumAgeMs = 5001 },
		func(r *modelv1.RiskSnapshotEvent) { r.ObservedAt = 0 },
		func(r *modelv1.RiskSnapshotEvent) { r.ExpiresAt = r.ObservedAt },
		func(r *modelv1.RiskSnapshotEvent) { r.ExpiresAt = r.ObservedAt + 5001 },
		func(r *modelv1.RiskSnapshotEvent) { r.ReasonCode = "ready" },
	} {
		candidate := proto.Clone(row).(*modelv1.RiskSnapshotEvent)
		change(candidate)
		if value, err := adminRiskEventValue(candidate); !errors.Is(err, domain.ErrAdminUnavailable) || value.ID != 0 {
			t.Fatal("invalid stored history escaped validation", err)
		}
	}
	row.MarketFeedId = nil
	row.Kind, row.SnapshotDeleted = string(domain.RiskSnapshotEventRemoved), true
	if value, err := adminRiskEventValue(row); err != nil || value.FeedID != nil || !value.SnapshotDeleted {
		t.Fatal("historical source removal or absent market binding was lost", err)
	}
	if _, err := adminRiskEventValue(nil); !errors.Is(err, domain.ErrAdminUnavailable) {
		t.Fatal("nil history accepted", err)
	}
	if _, err := NewAdminRiskEvents(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil risk history storage accepted", err)
	}
}
