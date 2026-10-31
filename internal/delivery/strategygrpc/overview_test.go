package strategygrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

type overviewRPCStore struct {
	calls  int
	token  string
	filter domain.AdminOverviewFilter
	value  domain.AdminStrategyOverview
	err    error
}

func (s *overviewRPCStore) Strategies(_ context.Context, token string, filter domain.AdminOverviewFilter) (domain.AdminStrategyOverview, error) {
	s.calls++
	s.token, s.filter = token, filter
	return s.value, s.err
}

func overviewRPCFixture() domain.AdminStrategyOverview {
	return domain.AdminStrategyOverview{
		ObservedAt:       time.UnixMilli(2000),
		Total:            9007199254740993,
		RequestedRunning: 1,
		NextAfterID:      9007199254740994,
		StoredStates: []domain.AdminStoredStateCount{{
			State: domain.StrategyStateRunning,
			Count: 9007199254740993,
		}},
		Items: []domain.AdminStrategyRuntime{{
			Strategy: domain.AdminStrategy{
				Spec: domain.StrategySpec{
					ID:                9007199254740994,
					Key:               "overview-example",
					AccountID:         "overview.account",
					InstrumentID:      1,
					Template:          domain.StrategyTemplateGrid,
					CodeVersion:       "test-example",
					Revision:          9007199254740995,
					State:             domain.StrategyStateRunning,
					DesiredState:      domain.StrategyStateStopped,
					RequestedVersion:  2,
					RequestedSource:   domain.MarketSourceOKX,
					ActiveVersion:     1,
					ActiveSource:      domain.MarketSourceBinance,
					Mode:              domain.MarketModeReplay,
					RuntimeOwnerID:    "legacy-holder",
					RuntimeOwnerEpoch: 9007199254740996,
				},
				CreatedAt: time.UnixMilli(1),
				UpdatedAt: time.UnixMilli(1000),
			},
			Lease: &domain.AdminLeaseObservation{
				Epoch:               9007199254740996,
				OwnerRecorded:       true,
				ExpiresAt:           time.UnixMilli(0),
				RuntimeMatchesLease: true,
			},
		}},
	}
}

func overviewRPCRequest() *strategyv1.GetStrategyOverviewRequest {
	return &strategyv1.GetStrategyOverviewRequest{
		Session:                   &strategyv1.AdminSession{Token: testSessionToken},
		Limit:                     1,
		AccountId:                 "overview.account",
		RequestedMarketDataSource: string(domain.MarketSourceOKX),
	}
}

func TestOverviewGRPCPreservesIndependentCountsTargetsAndReleasedLease(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &overviewRPCStore{value: overviewRPCFixture()}
	reader, err := NewOverviewQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), overviewRPCRequest())
	if err != nil || reply.Total != 9007199254740993 || reply.RequestedRunning != 1 || reply.Items[0].Strategy.RequestedMarketDataSource == *reply.Items[0].Strategy.ActiveMarketDataSource ||
		!reply.Items[0].Lease.OwnerRecorded || reply.Items[0].Lease.UnexpiredAtObservation || !reply.Items[0].Lease.RuntimeMatchesLease || reply.Items[0].Lease.ExpiresAtMs != 0 ||
		store.token != testSessionToken || store.filter.Source != domain.MarketSourceOKX || store.filter.AccountID != "overview.account" {
		t.Fatal("overview collapsed stored state/target, source or released holder", err)
	}
	encoded, err := proto.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.GetStrategyOverviewReply
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(reply, &decoded) {
		t.Fatal("overview lost exact typed values", err)
	}
	store.value.Items = nil // Cursor may exhaust the page without exhausting the scope.
	store.value.NextAfterID = 0
	request := overviewRPCRequest()
	request.AfterId = 9223372036854775807
	if reply, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), request); err != nil || reply.Total != store.value.Total || len(reply.Items) != 0 {
		t.Fatal("empty page erased full-scope counters", err)
	}
}

func TestOverviewGRPCRejectsUntrustedUnboundedUnknownAndInvalidRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &overviewRPCStore{}
	reader, err := NewOverviewQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOverviewQuery(nil); err == nil {
		t.Fatal("nil overview capability accepted")
	}
	if _, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServicePortfolio), overviewRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API peer read overview", err)
	}
	if _, err := reader.GetStrategyOverview(verified(t.Context(), rpcsecurity.ServiceAPI), overviewRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded overview accepted", err)
	}
	if _, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatal("nil overview request accepted", err)
	}
	for _, change := range []func(*strategyv1.GetStrategyOverviewRequest){
		func(r *strategyv1.GetStrategyOverviewRequest) { r.AfterId = -1 },
		func(r *strategyv1.GetStrategyOverviewRequest) { r.Limit = 101 },
		func(r *strategyv1.GetStrategyOverviewRequest) { r.AccountId = " bad" },
		func(r *strategyv1.GetStrategyOverviewRequest) {
			r.RequestedMarketDataSource = string(domain.MarketSourceBitget)
		},
		func(r *strategyv1.GetStrategyOverviewRequest) { r.AccountId = strings.Repeat("x", 2049) },
		func(r *strategyv1.GetStrategyOverviewRequest) { r.Session = nil },
		func(r *strategyv1.GetStrategyOverviewRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(r *strategyv1.GetStrategyOverviewRequest) {
			r.Session.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
	} {
		request := overviewRPCRequest()
		change(request)
		if _, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.Unauthenticated {
			t.Fatal("invalid overview request reached storage", err)
		}
	}
	if store.calls != 0 {
		t.Fatal("rejected overview touched storage")
	}
}

func TestOverviewGRPCRejectsInvalidCountsPrecisionAndLeaseSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &overviewRPCStore{}
	reader, err := NewOverviewQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminStrategyOverview){
		func(v *domain.AdminStrategyOverview) { v.Total-- },
		func(v *domain.AdminStrategyOverview) { v.RequestedRunning = v.Total + 1 },
		func(v *domain.AdminStrategyOverview) { v.StoredStates[0].State = "unknown" },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Strategy.Spec.AccountID = "other" },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Strategy.Spec.ActiveVersion = -1 },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Strategy.Spec.ActiveVersion = 0 },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Strategy.Spec.DesiredRunning = true },
		func(v *domain.AdminStrategyOverview) {
			v.Items[0].Strategy.Spec.RequestedSource = domain.MarketSourceBinance
		},
		func(v *domain.AdminStrategyOverview) { v.Items[0].Lease.UnexpiredAtObservation = true },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Lease.Epoch++ },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Lease.OwnerRecorded = false },
		func(v *domain.AdminStrategyOverview) { v.Items[0].Lease.ExpiresAt = time.Time{} },
		func(v *domain.AdminStrategyOverview) {
			v.Items[0].Lease.ExpiresAt = time.UnixMilli(0).Add(time.Nanosecond)
		},
		func(v *domain.AdminStrategyOverview) { v.ObservedAt = v.ObservedAt.Add(time.Nanosecond) },
		func(v *domain.AdminStrategyOverview) {
			v.Items[0].Strategy.UpdatedAt = v.Items[0].Strategy.UpdatedAt.Add(time.Nanosecond)
		},
	} {
		store.value = overviewRPCFixture()
		change(&store.value)
		if reply, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), overviewRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("malformed overview escaped response boundary", err)
		}
	}
	store.value, store.err = overviewRPCFixture(), errors.New("private overview read")
	if reply, err := reader.GetStrategyOverview(verified(ctx, rpcsecurity.ServiceAPI), overviewRPCRequest()); status.Code(err) != codes.Unavailable || status.Convert(err).Message() != domain.MessageAdminUnavailable || reply != nil {
		t.Fatal("private overview failure escaped", err)
	}
}
