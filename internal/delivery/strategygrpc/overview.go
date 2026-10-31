package strategygrpc

import (
	"context"
	"time"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// OverviewQueryServer borrows stored observations; it owns no workers or leases.
type OverviewQueryServer struct {
	strategyv1.UnimplementedStrategyOverviewQueryServer
	overview domain.AdminOverview
}

// NewOverviewQuery starts no work and grants no ownership or risk permission.
func NewOverviewQuery(overview domain.AdminOverview) (*OverviewQueryServer, error) {
	if overview == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &OverviewQueryServer{overview: overview}, nil
}

// GetStrategyOverview checks the current viewer through a single repository read.
func (s *OverviewQueryServer) GetStrategyOverview(ctx context.Context, request *strategyv1.GetStrategyOverviewRequest) (*strategyv1.GetStrategyOverviewReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminOverviewFilter{
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		AccountID: request.AccountId,
		Source:    domain.MarketSource(request.RequestedMarketDataSource),
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.overview.Strategies(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	reply, err := overviewProto(value, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	return reply, nil
}

func overviewProto(value domain.AdminStrategyOverview, filter domain.AdminOverviewFilter) (*strategyv1.GetStrategyOverviewReply, error) {
	if !overviewMillis(value.ObservedAt, false) || len(value.Items) > filter.Page.Limit || len(value.StoredStates) > 9 {
		return nil, domain.ErrAdminUnavailable
	}
	reply := &strategyv1.GetStrategyOverviewReply{
		ObservedAtMs:     value.ObservedAt.UnixMilli(),
		Total:            value.Total,
		RequestedRunning: value.RequestedRunning,
		StoredStates:     make([]*strategyv1.StoredStrategyStateCount, 0, len(value.StoredStates)),
		Items:            make([]*strategyv1.StrategyRuntimeObservation, 0, len(value.Items)),
		NextAfterId:      value.NextAfterID,
	}
	for _, entry := range value.StoredStates {
		reply.StoredStates = append(reply.StoredStates, &strategyv1.StoredStrategyStateCount{
			State: string(entry.State),
			Count: entry.Count,
		})
	}
	for _, entry := range value.Items {
		if !overviewMillis(entry.Strategy.CreatedAt, false) || !overviewMillis(entry.Strategy.UpdatedAt, false) ||
			entry.Strategy.Spec.DesiredRunning != (entry.Strategy.Spec.DesiredState == domain.StrategyStateRunning) ||
			entry.Strategy.Spec.ActiveVersion < 0 || (entry.Strategy.Spec.ActiveVersion == 0) != (entry.Strategy.Spec.ActiveSource == domain.MarketSourceUnspecified) {
			return nil, domain.ErrAdminUnavailable
		}
		item := &strategyv1.StrategyRuntimeObservation{Strategy: strategyProto(entry.Strategy)}
		if l := entry.Lease; l != nil {
			if !overviewMillis(l.ExpiresAt, true) {
				return nil, domain.ErrAdminUnavailable
			}
			item.Lease = &strategyv1.StrategyLeaseObservation{
				Epoch:                  l.Epoch,
				OwnerRecorded:          l.OwnerRecorded,
				ExpiresAtMs:            l.ExpiresAt.UnixMilli(),
				UnexpiredAtObservation: l.UnexpiredAtObservation,
				RuntimeMatchesLease:    l.RuntimeMatchesLease,
			}
		}
		reply.Items = append(reply.Items, item)
	}
	if !strategywire.ValidOverview(reply, filter) {
		return nil, domain.ErrAdminUnavailable
	}
	return reply, nil
}

func overviewMillis(value time.Time, allowEpoch bool) bool {
	ms := value.UnixMilli()
	return (ms > 0 || allowEpoch && ms == 0) && value.Equal(time.UnixMilli(ms))
}

var _ strategyv1.StrategyOverviewQueryServer = (*OverviewQueryServer)(nil)
