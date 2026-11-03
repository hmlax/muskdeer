package strategygrpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/deer/proto/rpc/strategy/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestEmergencyGRPCQueriesKeepHistoricalResultCurrentCountsAndExactScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &emergencyReadRPCFixture{value: emergencyRPCObservation()}
	reader, err := NewEmergencyQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	accepted := store.value.Record.Acceptance
	get := &strategyv1.GetEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		StrategyId:     accepted.StrategyID,
		CancellationId: accepted.ID,
	}
	reply, err := reader.GetEmergencyCancellation(ctx, get)
	deadline, _ := ctx.Deadline()
	if err != nil || reply.Record.State != string(domain.AdminEmergencyCancelFailed) || reply.Progress.Terminal != 1 || reply.Progress.Failed != 0 ||
		reply.Record.Revision != store.value.Record.Revision || store.strategyID != accepted.StrategyID || store.id != accepted.ID || store.deadline != deadline || store.token != testSessionToken {
		t.Fatal("RPC inferred a new result or lost exact query scope/budget", err)
	}
	page, err := reader.ListEmergencyCancellations(ctx, &strategyv1.ListEmergencyCancellationsRequest{
		Session:    get.Session,
		StrategyId: get.StrategyId,
		AfterId:    accepted.ID - 1,
		Limit:      1,
	})
	if err != nil || len(page.Items) != 1 || page.Items[0].Acceptance.Id != accepted.ID || store.page.AfterID != accepted.ID-1 || store.page.Limit != 1 || page.NextAfterId != 0 {
		t.Fatal("RPC changed history ordering or its supplied bounds", err)
	}
}

func TestEmergencyGRPCQueriesRejectPartialMalformedAndUnboundedResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = verified(ctx, rpcsecurity.ServiceAPI)
	store := &emergencyReadRPCFixture{}
	reader, err := NewEmergencyQuery(store)
	if err != nil {
		t.Fatal(err)
	}
	request := &strategyv1.GetEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: testSessionToken},
		StrategyId:     9007199254740995,
		CancellationId: 9007199254740993,
	}
	for _, change := range []func(*domain.AdminEmergencyCancelObservation){
		func(r *domain.AdminEmergencyCancelObservation) { r.Record.Acceptance.StrategyID++ },
		func(r *domain.AdminEmergencyCancelObservation) { r.Progress.Failed = 1 },
		func(r *domain.AdminEmergencyCancelObservation) { r.ObservedAt = r.ObservedAt.Add(time.Nanosecond) },
		func(r *domain.AdminEmergencyCancelObservation) {
			r.Record.CompletedAt = r.Record.CompletedAt.Add(time.Nanosecond)
		},
		func(r *domain.AdminEmergencyCancelObservation) { r.Record.State = "private-invalid-state" },
	} {
		store.value = emergencyRPCObservation()
		change(&store.value)
		if reply, err := reader.GetEmergencyCancellation(ctx, request); status.Code(err) != codes.Unavailable || reply != nil {
			t.Fatal("malformed stored observation escaped RPC", err)
		}
	}
	before := store.calls
	if _, err := reader.GetEmergencyCancellation(verified(t.Context(), rpcsecurity.ServiceAPI), request); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unbounded observation reached business read", err)
	}
	if _, err := reader.GetEmergencyCancellation(verified(ctx, rpcsecurity.ServicePortfolio), request); status.Code(err) != codes.PermissionDenied || store.calls != before {
		t.Fatal("unverified peer reached emergency read", err)
	}
}

type emergencyReadRPCFixture struct {
	domain.AdminEmergencyReader
	value      domain.AdminEmergencyCancelObservation
	calls      int
	strategyID int64
	id         int64
	page       domain.AdminPage
	deadline   time.Time
	token      string
}

func (s *emergencyReadRPCFixture) Observation(ctx context.Context, token string, strategyID, id int64) (domain.AdminEmergencyCancelObservation, error) {
	s.calls++
	s.token, s.strategyID, s.id = token, strategyID, id
	s.deadline, _ = ctx.Deadline()
	return s.value, nil
}

func (s *emergencyReadRPCFixture) History(ctx context.Context, token string, strategyID int64, page domain.AdminPage) (domain.AdminEmergencyCancelPage, error) {
	s.calls++
	s.token, s.strategyID, s.page = token, strategyID, page
	s.deadline, _ = ctx.Deadline()
	return domain.AdminEmergencyCancelPage{
		Items:      []domain.AdminEmergencyCancelRecord{s.value.Record},
		ObservedAt: s.value.ObservedAt,
	}, nil
}

func emergencyRPCObservation() domain.AdminEmergencyCancelObservation {
	return domain.AdminEmergencyCancelObservation{
		Record: domain.AdminEmergencyCancelRecord{
			Acceptance: domain.AdminEmergencyCancelAcceptance{
				ID:                   9007199254740993,
				ActorUserID:          1,
				StrategyID:           9007199254740995,
				Scope:                domain.AdminEmergencyCancelScopeStrategy,
				ExpectedRiskRevision: 1,
				RiskRevision:         2,
				Reason:               "confirmed strategy scope",
				CreatedAt:            time.UnixMilli(1700000000000),
			},
			State:              domain.AdminEmergencyCancelFailed,
			Revision:           9007199254740997,
			EnrollmentComplete: true,
			CompletedAt:        time.UnixMilli(1700000000001),
		},
		Progress: domain.AdminEmergencyCancelProgress{
			Targets:            1,
			Terminal:           1,
			EnrollmentComplete: true,
		},
		ObservedAt: time.UnixMilli(1700000000002),
	}
}
