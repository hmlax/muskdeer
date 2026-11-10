package console

import (
	"context"
	"slices"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm/clause"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// CreateAccount registers an existing SDK fund; it does not create or fund it,
// start a strategy, or overwrite any account's existing policy.
func (s *Server) CreateAccount(ctx context.Context, in *pb.CreateAccountRequest) (*pb.Account, error) {
	if err := s.authorize(ctx, in.GetActor(), true); err != nil {
		return nil, err
	}
	if in.GetActor().GetTenantId() == 0 {
		return nil, status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	if err := accountScope(in.GetActor()); err != nil {
		return nil, err
	}
	draft := in.GetData()
	user, e1 := exactAccountID(draft.GetUserId())
	fund, e2 := exactAccountID(draft.GetFundId())
	instrumentID, e3 := exactAccountID(draft.GetInstrumentId())
	if e1 != nil || e2 != nil || e3 != nil {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	account := native.ScopeKey(user, fund)
	if !slices.Contains(in.Actor.AllowedAccountIds, account) {
		return nil, status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	var sponsors int64
	if err := fresh(ctx, s.db).Model(&model.TradingAccountPolicy{}).
		Where("user_id = ? AND account_id IN ? AND account_id <> ? AND delete_at IS NULL", user, in.Actor.AllowedAccountIds, account).Count(&sponsors).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if sponsors == 0 {
		return nil, status.Error(codes.PermissionDenied, messageScopeRequired)
	}
	var product model.Instrument
	if err := fresh(ctx, s.db).Model(&model.Instrument{}).
		Where("id = ? AND status = ? AND execution_venue = ? AND delete_at IS NULL", instrumentID, domain.InstrumentStatusTrading, native.Venue).Take(&product).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	row := &model.TradingAccountPolicy{
		AccountId:            account,
		UserId:               user,
		FundId:               fund,
		Symbol:               product.Symbol,
		MarketDataSource:     draft.MarketDataSource,
		PriceTick:            product.PriceTick,
		QuantityStep:         product.QuantityStep,
		MaximumNotional:      draft.MaximumNotional,
		MaximumOrderNotional: draft.MaximumOrderNotional,
		Leverage:             draft.Leverage,
		Enabled:              true,
		CutoverCompleted:     true,
		ObservedNotional:     "0",
		ReservedNotional:     "0",
		AvailableQuote:       "0",
		WorstFeeRate:         "0",
	}
	profile, err := venueconfig.ProfileFromBudget(row)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, messageInvalid)
	}
	inspect := s.accountInspector
	if inspect == nil {
		inspect = s.inspectVenueAccount
	}
	snapshot, err := inspect(ctx, native.Scope(profile))
	if err != nil {
		return nil, err
	}
	if snapshot.GetScope().GetUserId() != user || snapshot.GetScope().GetFundId() != fund || snapshot.GetScope().GetSymbol() != product.Symbol || snapshot.GetValidUntilMs() <= time.Now().UnixMilli() || snapshot.GetMarket().GetPriceTick() != profile.PriceTick || snapshot.GetMarket().GetQuantityStep() != profile.QuantityStep || profile.Leverage > snapshot.GetMarket().GetMaximumLeverage() {
		return nil, status.Error(codes.FailedPrecondition, messageInvalid)
	}
	if err := fresh(ctx, s.db).Model(&model.TradingAccountPolicy{}).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "account_id"}},
			DoNothing: true,
		}).Create(row).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	var saved model.TradingAccountPolicy
	if err := fresh(ctx, s.db).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", account).Take(&saved).Error; err != nil {
		return nil, s.fail(ctx, err)
	}
	if !sameAccountPolicy(&saved, profile) {
		return nil, status.Error(codes.AlreadyExists, messageInvalid)
	}
	return accountDTO(&saved), nil
}

func exactAccountID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != value {
		return 0, status.Error(codes.InvalidArgument, messageInvalid)
	}
	return id, nil
}

func sameAccountPolicy(row *model.TradingAccountPolicy, expected *common.VenueProfilePolicy) bool {
	profile, err := venueconfig.ProfileFromBudget(row)
	return err == nil && row.Enabled && profile.UserId == expected.UserId && profile.FundId == expected.FundId && profile.Symbol == expected.Symbol && profile.MarketDataSource == expected.MarketDataSource && profile.Leverage == expected.Leverage && profile.MaximumNotional == expected.MaximumNotional && profile.MaximumOrderNotional == expected.MaximumOrderNotional && profile.PriceTick == expected.PriceTick && profile.QuantityStep == expected.QuantityStep
}

func (s *Server) inspectVenueAccount(ctx context.Context, scope *common.VenueScope) (*common.VenueSnapshot, error) {
	if s.factory == nil {
		return nil, status.Error(codes.Unavailable, messageUnavailable)
	}
	connection, err := s.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		return nil, err
	}
	return execution.NewExecutionClient(connection).InspectAccount(ctx, scope)
}
