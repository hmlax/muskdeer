package venue

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func (r *Runtime) acquireSynthetic(ctx context.Context, symbol string) (domain.LeaseToken, error) {
	if r.store.matching {
		return domain.LeaseToken{Key: domain.LeaseKey{AccountID: native.SyntheticExecutionKey(r.cfg.Venue.SyntheticTape.RunId, symbol)}}, ctx.Err()
	}
	return native.KeepLease(ctx, r.leases, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.SyntheticExecutionKey(r.cfg.Venue.SyntheticTape.RunId, symbol),
	}, r.owner, r.cfg.Venue.LeaseTtl.AsDuration())
}

// PublishSynthetic emits development market data only. Execution refreshes the
// source and prices the slot again; the reply carries the actual emitted price.
func (r *Runtime) PublishSynthetic(ctx context.Context, proposal *commonv1.SyntheticTapePlan) (*commonv1.SyntheticTapeResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if r.closing.Load() || r.client.Load() == nil || !r.cfg.Venue.GetSyntheticTape().GetEnabled() {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	if proposal.GetRunId() != r.cfg.Venue.SyntheticTape.RunId || proposal.GetStrategy().GetAccountId() != native.SyntheticStrategyKey(proposal.RunId, proposal.GetSymbol()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
	defer cancel()
	book, err := r.book(op, proposal.Symbol)
	if err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	plan, err := r.syntheticPlan(op, proposal, book)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if err := r.client.Load().CheckSyntheticMarket(op, native.SyntheticMarket(r.cfg.Venue, plan.Symbol), plan); err != nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	lease, err := r.acquireSynthetic(op, plan.Symbol)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	result, err := r.store.PublishSynthetic(op, lease, r.cfg.Venue, plan, r.client.Load().PublishSynthetic)
	if err != nil {
		code := codes.FailedPrecondition
		if errors.Is(err, ErrRiskLimit) {
			code = codes.ResourceExhausted
		}
		return nil, status.Error(code, native.MessageInvalid)
	}
	level := slog.LevelInfo
	if result.State == native.SyntheticUnknown {
		level = slog.LevelWarn
	}
	if err := r.logger.LogAttrs(ctx, level, result.State, slog.String("symbol", plan.Symbol), slog.String("run_id", plan.RunId), slog.Uint64("sequence", uint64(plan.Sequence)), slog.Bool("replayed", result.Replayed)); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) syntheticPlan(ctx context.Context, proposal *commonv1.SyntheticTapePlan, book *commonv1.VenueBook) (*commonv1.SyntheticTapePlan, error) {
	if r.cfg.Venue.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK {
		if proposal.SourceTrade != nil {
			return nil, ErrInvalidSubmit
		}
		plan, err := native.SyntheticPlan(r.cfg.Venue, proposal.Symbol, proposal.Sequence, book, proposal.Strategy)
		if err != nil || proposal.Side != plan.GetSide() || proposal.Quantity != plan.GetQuantity() {
			return nil, ErrInvalidSubmit
		}
		plan.StrategyInstanceId, plan.StrategyRevision = proposal.StrategyInstanceId, proposal.StrategyRevision
		return plan, nil
	}
	// An aggregate-mode client must explicitly supply that source intent. The
	// actual sample can advance before execution reads the authoritative stream.
	expected, err := native.SyntheticSourcePlan(r.cfg.Venue, proposal)
	if err != nil || !proto.Equal(expected, proposal) {
		return nil, ErrInvalidSubmit
	}
	connection, err := r.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return nil, err
	}
	trade, err := marketvenuev1.NewMarketClient(connection).GetTrade(ctx, &marketvenuev1.TradeRequest{Symbol: proposal.Symbol})
	if err != nil {
		return nil, err
	}
	return native.SyntheticSourcePlan(r.cfg.Venue, &commonv1.SyntheticTapePlan{
		RunId:              proposal.RunId,
		Symbol:             proposal.Symbol,
		Sequence:           proposal.Sequence,
		Book:               book,
		Strategy:           proposal.Strategy,
		SourceTrade:        trade,
		StrategyInstanceId: proposal.StrategyInstanceId,
		StrategyRevision:   proposal.StrategyRevision,
	})
}

func (r *Runtime) renewSyntheticScopes(ctx context.Context) error {
	if r.store.matching {
		return ctx.Err()
	}
	if !r.cfg.Venue.GetSyntheticTape().GetEnabled() {
		return nil
	}
	for _, profile := range r.cfg.Venue.SyntheticTape.Profiles {
		op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
		_, err := r.acquireSynthetic(op, profile.Symbol)
		cancel()
		if err != nil && !errors.Is(err, domain.ErrLeaseHeld) {
			return err
		}
	}
	return nil
}
