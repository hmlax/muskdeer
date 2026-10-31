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

type riskEventRPCStore struct {
	calls  int
	token  string
	filter domain.AdminRiskEventFilter
	value  domain.AdminRiskEvent
	err    error
}

func (s *riskEventRPCStore) List(_ context.Context, token string, filter domain.AdminRiskEventFilter) (domain.AdminRiskEventPage, error) {
	s.calls++
	s.token, s.filter = token, filter
	return domain.AdminRiskEventPage{Items: []domain.AdminRiskEvent{s.value}}, s.err
}

func (s *riskEventRPCStore) Detail(_ context.Context, token string, _ int64) (domain.AdminRiskEvent, error) {
	s.calls++
	s.token = token
	return s.value, s.err
}

func riskEventRPCFixture() domain.AdminRiskEvent {
	return domain.AdminRiskEvent{
		ID:                   9007199254740993,
		RecordedAt:           time.UnixMilli(1),
		Kind:                 domain.RiskSnapshotEventChanged,
		SnapshotID:           9007199254740994,
		SnapshotRevision:     9007199254740995,
		StrategyID:           9007199254740996,
		AccountID:            "risk.account",
		InstrumentID:         9007199254740997,
		ConfigVersion:        9007199254740998,
		Source:               domain.MarketSourceOKX,
		PublisherEpoch:       9007199254740999,
		MarketSequence:       9007199254741000,
		PublicationRevision:  9007199254741001,
		SourceVersion:        9007199254741002,
		MarketMaximumAge:     5 * time.Second,
		ObservedAt:           time.UnixMilli(1000),
		ExpiresAt:            time.UnixMilli(2000),
		RecordedAllowNewRisk: true,
		Reason:               domain.RiskReasonReady,
	}
}

func riskEventRPCRequest() *strategyv1.ListRiskEventsRequest {
	value := riskEventRPCFixture()
	return &strategyv1.ListRiskEventsRequest{
		Session:          &strategyv1.AdminSession{Token: testSessionToken},
		Limit:            1,
		StrategyId:       value.StrategyID,
		AccountId:        value.AccountID,
		InstrumentId:     value.InstrumentID,
		MarketDataSource: string(value.Source),
		Kind:             string(value.Kind),
		ReasonCode:       string(value.Reason),
	}
}

func TestRiskEventsGRPCPreservesRawExpiredHistoryAndOptionalIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &riskEventRPCStore{value: riskEventRPCFixture()}
	reader, err := NewRiskEventQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := reader.ListRiskEvents(verified(ctx, rpcsecurity.ServiceAPI), riskEventRPCRequest())
	if err != nil || len(reply.Events) != 1 || reply.Events[0].RecordedReconciled || !reply.Events[0].RecordedAllowNewRisk ||
		reply.Events[0].MarketSequence != store.value.MarketSequence || reply.Events[0].MarketFeedId != nil ||
		reply.Events[0].RecordedAtMs != 1 || reply.Events[0].ExpiresAtMs != 2000 || store.token != testSessionToken || store.filter.Reason != domain.RiskReasonReady {
		t.Fatal("RPC reinterpreted old flags, rounded sequence or fabricated market binding", err)
	}
	encoded, err := proto.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	var decoded strategyv1.ListRiskEventsReply
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(reply, &decoded) {
		t.Fatal("risk diagnostic failed typed protobuf roundtrip", err)
	}
	store.value.Kind, store.value.SnapshotDeleted = domain.RiskSnapshotEventRemoved, true
	store.value.FeedID = proto.Int64(9007199254741003)
	detail, err := reader.GetRiskEvent(verified(ctx, rpcsecurity.ServiceAPI), &strategyv1.GetRiskEventRequest{
		Session: riskEventRPCRequest().Session,
		EventId: store.value.ID,
	})
	if err != nil || !detail.Event.SnapshotDeleted || detail.Event.MarketFeedId == nil || *detail.Event.MarketFeedId != *store.value.FeedID {
		t.Fatal("removed source or optional historical feed lost", err)
	}
	*detail.Event.MarketFeedId = 1
	if *store.value.FeedID != 9007199254741003 {
		t.Fatal("transmission aliases domain identity pointer")
	}
}

func TestRiskEventsGRPCRejectsWrongPeerUnboundedAndAmbiguousRequests(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	store := &riskEventRPCStore{}
	reader, err := NewRiskEventQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ListRiskEvents(verified(ctx, rpcsecurity.ServicePortfolio), riskEventRPCRequest()); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API peer read historical risk", err)
	}
	if _, err := reader.ListRiskEvents(verified(t.Context(), rpcsecurity.ServiceAPI), riskEventRPCRequest()); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded risk read accepted", err)
	}
	for _, change := range []func(*strategyv1.ListRiskEventsRequest){
		func(r *strategyv1.ListRiskEventsRequest) { r.Limit = 101 },
		func(r *strategyv1.ListRiskEventsRequest) { r.AfterId = -1 },
		func(r *strategyv1.ListRiskEventsRequest) { r.StrategyId = -1 },
		func(r *strategyv1.ListRiskEventsRequest) { r.InstrumentId = -1 },
		func(r *strategyv1.ListRiskEventsRequest) { r.AccountId = " risk.account" },
		func(r *strategyv1.ListRiskEventsRequest) { r.MarketDataSource = string(domain.MarketSourceBitget) },
		func(r *strategyv1.ListRiskEventsRequest) { r.Kind = "changed" },
		func(r *strategyv1.ListRiskEventsRequest) { r.ReasonCode = "ready" },
		func(r *strategyv1.ListRiskEventsRequest) { r.Session = nil },
		func(r *strategyv1.ListRiskEventsRequest) { r.AccountId = strings.Repeat("a", 2049) },
		func(r *strategyv1.ListRiskEventsRequest) { r.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(r *strategyv1.ListRiskEventsRequest) { r.Session.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
	} {
		request := riskEventRPCRequest()
		change(request)
		if _, err := reader.ListRiskEvents(verified(ctx, rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.Unauthenticated {
			t.Fatal("invalid risk history request reached storage", err)
		}
	}
	if store.calls != 0 {
		t.Fatal("rejected risk read touched storage")
	}
}

func TestRiskEventsGRPCRejectsMismatchedOrInvalidSuccessfulHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &riskEventRPCStore{}
	reader, err := NewRiskEventQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.AdminRiskEvent){
		func(v *domain.AdminRiskEvent) { v.ID = 0 },
		func(v *domain.AdminRiskEvent) { v.StrategyID++ },
		func(v *domain.AdminRiskEvent) { v.AccountID = "different.account" },
		func(v *domain.AdminRiskEvent) { v.InstrumentID++ },
		func(v *domain.AdminRiskEvent) { v.Source = domain.MarketSourceBinance },
		func(v *domain.AdminRiskEvent) { v.Kind = domain.RiskSnapshotEventBaseline },
		func(v *domain.AdminRiskEvent) { v.Reason = domain.RiskReasonBudget },
		func(v *domain.AdminRiskEvent) { v.ExpiresAt = v.ObservedAt },
		func(v *domain.AdminRiskEvent) { v.MarketMaximumAge += time.Nanosecond },
		func(v *domain.AdminRiskEvent) { v.RecordedAt = v.RecordedAt.Add(time.Nanosecond) },
	} {
		store.value = riskEventRPCFixture()
		change(&store.value)
		if reply, err := reader.ListRiskEvents(ctx, riskEventRPCRequest()); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("invalid or filtered success escaped typed risk response", err)
		}
	}
	store.value = riskEventRPCFixture()
	if reply, err := reader.GetRiskEvent(ctx, &strategyv1.GetRiskEventRequest{
		Session: riskEventRPCRequest().Session,
		EventId: store.value.ID + 1,
	}); status.Code(err) != codes.Unavailable || reply != nil {
		t.Fatal("risk detail returned another identity", err)
	}
	store.err = errors.New("private risk read failure")
	if reply, err := reader.ListRiskEvents(ctx, riskEventRPCRequest()); status.Code(err) != codes.Unavailable || status.Convert(err).Message() != domain.MessageAdminUnavailable || reply != nil {
		t.Fatal("private storage error escaped RPC", err)
	}
}
