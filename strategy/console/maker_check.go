package console

import (
	"context"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonmodel "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/hmlax/muskdeer/strategy/native"
)

// CheckInstance inspects one scoped maker on demand; listing never polls SDK accounts.
func (s *Server) CheckInstance(ctx context.Context, in *pb.InstanceRequest) (*pb.MakerCheck, error) {
	if err := s.authorize(ctx, in.GetActor(), false); err != nil {
		return nil, err
	}
	row, err := s.row(ctx, in.GetActor(), in.GetId())
	if err != nil {
		return nil, err
	}
	if domain.StrategyTemplate(row.Template) != domain.StrategyTemplateMarketMaker {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	result, _, err := s.checkMaker(ctx, row)
	return result, err
}

func (s *Server) checkMaker(ctx context.Context, row *model.StrategyInstance) (*pb.MakerCheck, *instanceconfig.StartCheck, error) {
	var budget commonmodel.TradingAccountPolicy
	if err := fresh(ctx, s.db).Model(&commonmodel.TradingAccountPolicy{}).Where("account_id = ? AND delete_at IS NULL", row.AccountId).Take(&budget).Error; err != nil {
		return nil, nil, s.fail(ctx, err)
	}
	profile, err := venueconfig.ProfileFromBudget(&budget)
	if err != nil {
		return nil, nil, status.Error(codes.FailedPrecondition, domain.MakerTargetsMessage)
	}
	profile.StrategyInstanceId = row.Id
	result := &pb.MakerCheck{
		MaximumNotional:      profile.MaximumNotional,
		MaximumOrderNotional: profile.MaximumOrderNotional,
		CheckedAtMs:          time.Now().UnixMilli(),
		Revision:             row.Revision,
		ControlVersion:       row.RequestedConfigVersion,
	}
	if !budget.Enabled {
		return blockedMaker(result, domain.AccountDisabledReason, domain.AccountDisabledMessage), nil, nil
	}
	inspect := s.accountInspector
	if inspect == nil {
		inspect = s.inspectVenueAccount
	}
	snapshot, err := inspect(ctx, values.Scope(profile))
	if err != nil {
		return blockedMaker(result, domain.MakerSnapshotReason, domain.MakerSnapshotMessage), nil, nil
	}
	book, err := s.makerBook(ctx, profile.Symbol)
	if err != nil {
		return blockedMaker(result, domain.MakerSourceReason, domain.MakerSourceMessage), nil, nil
	}
	result = evaluateMaker(profile, row.Parameters.GetMarketMaker(), book, snapshot, time.Now().UnixMilli())
	result.Revision, result.ControlVersion = row.Revision, row.RequestedConfigVersion
	if !result.CanStart {
		return result, nil, nil
	}
	view, err := s.instance(ctx, row, s.inventories(ctx))
	if err != nil {
		return nil, nil, err
	}
	if view.RestartRequired {
		return blockedMaker(result, domain.MakerRevisionReason, domain.MakerRevisionMessage), nil, nil
	}
	if result.ValidUntilMs <= time.Now().UnixMilli() {
		return blockedMaker(result, domain.MakerCheckExpiredReason, domain.MakerCheckExpiredMessage), nil, nil
	}
	proof := &instanceconfig.StartCheck{
		PolicyUpdatedAt:  budget.UpdateAt,
		Profile:          proto.CloneOf(profile),
		RequiredNotional: result.TargetNotional,
		ValidUntilMs:     result.ValidUntilMs,
	}
	return result, proof, nil
}

func (s *Server) makerBook(ctx context.Context, symbol string) (*common.VenueBook, error) {
	if s.bookInspector != nil {
		return s.bookInspector(ctx, symbol)
	}
	if s.factory == nil {
		return nil, status.Error(codes.Unavailable, messageUnavailable)
	}
	conn, err := s.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		return nil, err
	}
	return market.NewMarketClient(conn).GetBook(ctx, &market.BookRequest{Symbol: symbol})
}

func evaluateMaker(profile *common.VenueProfilePolicy, params *common.DepthMakerParameters, book *common.VenueBook, snapshot *common.VenueSnapshot, now int64) *pb.MakerCheck {
	result := &pb.MakerCheck{
		MaximumNotional:      profile.MaximumNotional,
		MaximumOrderNotional: profile.MaximumOrderNotional,
		CheckedAtMs:          now,
	}
	if !proto.Equal(snapshot.GetScope(), values.Scope(profile)) || snapshot.GetValidUntilMs() <= now {
		return blockedMaker(result, domain.MakerSnapshotReason, domain.MakerSnapshotMessage)
	}
	result.SnapshotObservedAtMs, result.AvailableQuote = snapshot.ObservedAtMs, snapshot.Available
	prefix := values.InstanceClientPrefix(profile)
	for _, order := range snapshot.Orders {
		if !strings.HasPrefix(order.ClientId, prefix) {
			continue
		}
		switch domain.Side(order.Side) {
		case domain.SideBuy:
			result.ActiveBuyOrders++
		case domain.SideSell:
			result.ActiveSellOrders++
		}
	}
	if book.GetValidUntilMs() <= now || len(book.GetBids()) == 0 || len(book.GetAsks()) == 0 || book.GetSource() != profile.MarketDataSource {
		return blockedMaker(result, domain.MakerSourceReason, domain.MakerSourceMessage)
	}
	if params == nil || params.Depth == 0 {
		return blockedMaker(result, domain.MakerTargetsReason, domain.MakerTargetsMessage)
	}
	targets, err := native.BuildTargets(&common.VenuePolicy{
		Depth:            params.Depth,
		QuantityFraction: params.QuantityFraction,
	}, profile, book, snapshot)
	if err != nil {
		return blockedMaker(result, domain.MakerTargetsReason, domain.MakerTargetsMessage)
	}
	required := decimal.Zero
	for _, target := range targets {
		price, e1 := values.Value(target.Price, false, false)
		quantity, e2 := values.Value(target.Quantity, false, false)
		if e1 != nil || e2 != nil {
			return blockedMaker(result, domain.MakerTargetsReason, domain.MakerTargetsMessage)
		}
		required = required.Add(price.Mul(quantity))
	}
	result.TargetNotional, result.TargetOrderCount = required.String(), uint32(len(targets))
	result.ValidUntilMs = min(book.ValidUntilMs, snapshot.ValidUntilMs)
	maximum, err := values.Value(profile.MaximumNotional, false, false)
	if err != nil {
		return blockedMaker(result, domain.MakerTargetsReason, domain.MakerTargetsMessage)
	}
	if required.GreaterThan(maximum) {
		return blockedMaker(result, domain.MakerBudgetReason, domain.MakerBudgetMessage)
	}
	result.CanStart, result.Reason, result.Message = true, domain.MakerReadyReason, domain.MakerReadyMessage
	return result
}

func blockedMaker(result *pb.MakerCheck, reason, message string) *pb.MakerCheck {
	result.CanStart, result.Reason, result.Message = false, reason, message
	return result
}

func makerCheckError(reason, message string) error {
	failure, err := status.New(codes.FailedPrecondition, message).WithDetails(&errdetails.ErrorInfo{Reason: reason})
	if err != nil {
		return status.Error(codes.FailedPrecondition, message)
	}
	return failure.Err()
}
