package himalaya

import (
	"context"
	"errors"
	"log/slog"

	"github.com/shopspring/decimal"
	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	rpc "github.com/hmlax/muskdeer/deer/proto/rpc/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func (r *Runtime) acquireSynthetic(ctx context.Context, symbol string) (domain.LeaseToken, error) {
	token, err := r.leases.Acquire(ctx, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.SyntheticExecutionKey(r.cfg.Himalaya.SyntheticTape.RunId, symbol),
	}, r.owner, r.cfg.Himalaya.LeaseTtl.AsDuration())
	if err != nil {
		return domain.LeaseToken{}, err
	}
	return r.leases.Renew(ctx, token, r.cfg.Himalaya.LeaseTtl.AsDuration())
}

// PublishSynthetic emits development market data only. Execution refreshes the
// source and prices the slot again; the reply carries the actual emitted price.
func (r *Runtime) PublishSynthetic(ctx context.Context, proposal *commonv1.SyntheticTapePlan) (*commonv1.SyntheticTapeResult, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	if r.closing.Load() || r.client.Load() == nil || !r.cfg.Himalaya.GetSyntheticTape().GetEnabled() {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	if proposal.GetRunId() != r.cfg.Himalaya.SyntheticTape.RunId || proposal.GetStrategy().GetAccountId() != native.SyntheticStrategyKey(proposal.RunId, proposal.GetSymbol()) {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout.AsDuration())
	defer cancel()
	book, err := r.book(op, proposal.Symbol)
	if err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	plan, err := r.syntheticPlan(op, proposal, book)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, native.MessageInvalid)
	}
	if err := r.client.Load().checkSyntheticMarket(op, native.SyntheticMarket(r.cfg.Himalaya, plan.Symbol), plan); err != nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	lease, err := r.acquireSynthetic(op, plan.Symbol)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, native.MessageInvalid)
	}
	result, err := r.store.PublishSynthetic(op, lease, r.cfg.Himalaya, plan, r.client.Load().publishSynthetic)
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

func (r *Runtime) syntheticPlan(ctx context.Context, proposal *commonv1.SyntheticTapePlan, book *commonv1.HimalayaBook) (*commonv1.SyntheticTapePlan, error) {
	if r.cfg.Himalaya.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_BOOK {
		if proposal.SourceTrade != nil {
			return nil, ErrInvalidSubmit
		}
		plan, err := native.SyntheticPlan(r.cfg.Himalaya, proposal.Symbol, proposal.Sequence, book, proposal.Strategy)
		if err != nil || proposal.Side != plan.GetSide() || proposal.Quantity != plan.GetQuantity() {
			return nil, ErrInvalidSubmit
		}
		return plan, nil
	}
	// An aggregate-mode client must explicitly supply that source intent. The
	// actual sample can advance before execution reads the authoritative stream.
	expected, err := native.SyntheticSourcePlan(r.cfg.Himalaya, proposal)
	if err != nil || !proto.Equal(expected, proposal) {
		return nil, ErrInvalidSubmit
	}
	connection, err := r.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return nil, err
	}
	trade, err := rpc.NewMarketClient(connection).GetTrade(ctx, &rpc.TradeRequest{Symbol: proposal.Symbol})
	if err != nil {
		return nil, err
	}
	return native.SyntheticSourcePlan(r.cfg.Himalaya, &commonv1.SyntheticTapePlan{
		RunId:       proposal.RunId,
		Symbol:      proposal.Symbol,
		Sequence:    proposal.Sequence,
		Book:        book,
		Strategy:    proposal.Strategy,
		SourceTrade: trade,
	})
}

func (c *Client) publishSynthetic(ctx context.Context, input *pb.WashTradeRequest) (*pb.WashTradeResponse, error) {
	if c == nil || c.sdk == nil || c.guard == nil {
		return nil, ErrInvalidSubmit
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.sdk.WashTrade(op, input)
}

func (c *Client) checkSyntheticMarket(ctx context.Context, profile *configv1.HimalayaProfile, plan *commonv1.SyntheticTapePlan) error {
	if c == nil || c.sdk == nil || profile == nil {
		return ErrInvalidSubmit
	}
	response, err := c.sdk.Raw().GetMarket(ctx, &pb.GetMarketRequest{Symbol: plan.Symbol})
	if err != nil {
		return err
	}
	market := response.GetMarket()
	if market.GetSymbol() != plan.Symbol || !response.GetHealth().GetOk() || market.Status != 1 || market.EffectiveBasePrecision > 18 || market.EffectiveQuotePrecision > 18 {
		return ErrInvalidSnapshot
	}
	base, quote := -int32(market.EffectiveBasePrecision), -int32(market.EffectiveQuotePrecision)
	tick := decimal.NewFromInt(market.PriceTickTicks).Shift(quote)
	step := decimal.NewFromInt(market.BaseIncrement).Shift(base)
	if tick.String() != profile.PriceTick || step.String() != profile.QuantityStep {
		return ErrInvalidSnapshot
	}
	quantity, e1 := native.Value(plan.Quantity, false, false)
	price, e2 := native.Value(plan.Price, false, false)
	if e1 != nil || e2 != nil || quantity.LessThan(decimal.NewFromInt(market.BaseMinSize).Shift(base)) ||
		market.BaseMaxSize > 0 && quantity.GreaterThan(decimal.NewFromInt(market.BaseMaxSize).Shift(base)) ||
		price.Mul(quantity).LessThan(decimal.NewFromInt(market.QuoteMinSize).Shift(-8)) {
		return ErrInvalidSubmit
	}
	return nil
}

func (r *Runtime) renewSyntheticScopes(ctx context.Context) error {
	if !r.cfg.Himalaya.GetSyntheticTape().GetEnabled() {
		return nil
	}
	for _, profile := range r.cfg.Himalaya.SyntheticTape.Profiles {
		op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout.AsDuration())
		_, err := r.acquireSynthetic(op, profile.Symbol)
		cancel()
		if err != nil && !errors.Is(err, domain.ErrLeaseHeld) {
			return err
		}
	}
	return nil
}
