package venue

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func (r *Runtime) currentWashClient(ctx context.Context, id, revision int64, symbol string) (gateway.WashClient, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if r.closing.Load() || r.client.Load() == nil || r.cfg.Template != domain.StrategyTemplateWashTrade || r.cfg.InstanceID <= 0 || !r.cfg.Venue.GetSyntheticTape().GetDevelopmentOnly() || id != r.cfg.InstanceID || revision != r.cfg.Revision || native.SyntheticMarket(r.cfg.Venue, symbol) == nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	control := r.washControl.Load()
	if control == nil || !control.active || time.Since(control.observed) > washControlAge {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	client, ok := r.client.Load().Client.(gateway.WashClient)
	if !ok {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	return client, nil
}

// GetWashMarket exposes expiring venue rules, independently of account readiness.
func (r *Runtime) GetWashMarket(ctx context.Context, input *pb.WashMarketRequest) (*pb.WashMarketRules, error) {
	client, err := r.currentWashClient(ctx, input.GetStrategyInstanceId(), input.GetStrategyRevision(), input.GetSymbol())
	if err != nil {
		return nil, err
	}
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
	defer cancel()
	rules, err := client.WashMarket(op, input.Symbol)
	if err != nil || rules == nil || !time.Now().Before(rules.ValidUntil) {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	return &pb.WashMarketRules{
		Market:       rules.Market,
		ValidUntilMs: rules.ValidUntil.UnixMilli(),
	}, nil
}
