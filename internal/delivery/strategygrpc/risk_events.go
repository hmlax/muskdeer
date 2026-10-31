package strategygrpc

import (
	"context"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/strategywire"
)

// RiskEventQueryServer serves historical diagnostics through the API peer only.
type RiskEventQueryServer struct {
	strategyv1.UnimplementedStrategyRiskEventQueryServer
	events domain.AdminRiskEvents
}

// NewRiskEventQuery borrows reads without evaluating or granting current risk.
func NewRiskEventQuery(events domain.AdminRiskEvents) (*RiskEventQueryServer, error) {
	if events == nil {
		return nil, domain.ErrInvalidAdminQuery
	}
	return &RiskEventQueryServer{events: events}, nil
}

// ListRiskEvents returns bounded immutable recordings, including retired scopes.
func (s *RiskEventQueryServer) ListRiskEvents(ctx context.Context, request *strategyv1.ListRiskEventsRequest) (*strategyv1.ListRiskEventsReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	filter := domain.AdminRiskEventFilter{
		Page: domain.AdminPage{
			AfterID: request.AfterId,
			Limit:   int(request.Limit),
		},
		StrategyID:   request.StrategyId,
		AccountID:    request.AccountId,
		InstrumentID: request.InstrumentId,
		Source:       domain.MarketSource(request.MarketDataSource),
		Kind:         domain.RiskSnapshotEventKind(request.Kind),
		Reason:       domain.RiskReason(request.ReasonCode),
	}
	if err := filter.Validate(); err != nil {
		return nil, rpcError(err)
	}
	page, err := s.events.List(ctx, request.Session.Token, filter)
	if err != nil {
		return nil, rpcError(err)
	}
	if len(page.Items) > filter.Page.Limit {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	reply := &strategyv1.ListRiskEventsReply{
		Events:      make([]*strategyv1.RiskDiagnosticEvent, 0, len(page.Items)),
		NextAfterId: page.NextAfterID,
	}
	for _, event := range page.Items {
		value, err := strategywire.RiskEventProto(event)
		if err != nil {
			return nil, rpcError(err)
		}
		reply.Events = append(reply.Events, value)
	}
	if !strategywire.ValidRiskEventPage(reply, filter) {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return reply, nil
}

// GetRiskEvent binds the response to the immutable recording ID.
func (s *RiskEventQueryServer) GetRiskEvent(ctx context.Context, request *strategyv1.GetRiskEventRequest) (*strategyv1.GetRiskEventReply, error) {
	if err := boundary(ctx, request); err != nil {
		return nil, err
	}
	if err := sessionValid(request.Session); err != nil {
		return nil, rpcError(err)
	}
	if request.EventId <= 0 {
		return nil, rpcError(domain.ErrInvalidAdminQuery)
	}
	event, err := s.events.Detail(ctx, request.Session.Token, request.EventId)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := strategywire.RiskEventProto(event)
	if err != nil || value.Id != request.EventId {
		return nil, rpcError(domain.ErrAdminUnavailable)
	}
	return &strategyv1.GetRiskEventReply{Event: value}, nil
}

var _ strategyv1.StrategyRiskEventQueryServer = (*RiskEventQueryServer)(nil)
