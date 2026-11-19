package native

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// targetRules belongs to one strategy worker. It never stores funds, orders,
// positions or account freshness, and never authorizes an SDK operation.
type targetRules struct {
	scope  *common.VenueScope
	market *common.VenueMarket
}

func (rules *targetRules) targets(ctx context.Context, cfg *venueconfig.RuntimeConfig, profile *common.VenueProfilePolicy, book *common.VenueBook, snapshot *common.VenueSnapshot, readErr error) ([]*common.VenueTarget, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	maker := cfg.InstanceID > 0 && cfg.Template == domain.StrategyTemplateMarketMaker && cfg.Grid == nil && cfg.LongGrid == nil
	if !maker {
		if readErr != nil {
			return nil, readErr
		}
		return instanceTargets(cfg, profile, book, snapshot)
	}
	if book.GetValidUntilMs() <= time.Now().UnixMilli() {
		return nil, values.ErrInvalid
	}
	if readErr != nil {
		if status.Code(readErr) != codes.Unavailable || rules.market == nil || !proto.Equal(rules.scope, values.Scope(profile)) {
			return nil, readErr
		}
		return buildTargets(cfg.Venue, profile, book, rules.market)
	}
	if snapshot.GetValidUntilMs() <= time.Now().UnixMilli() {
		return nil, values.ErrInvalid
	}
	targets, err := BuildTargets(cfg.Venue, profile, book, snapshot)
	if err != nil {
		return nil, err
	}
	rules.scope = proto.CloneOf(snapshot.Scope)
	rules.market = &common.VenueMarket{
		PriceTick:       snapshot.Market.PriceTick,
		QuantityStep:    snapshot.Market.QuantityStep,
		MinimumQuantity: snapshot.Market.MinimumQuantity,
		MaximumQuantity: snapshot.Market.MaximumQuantity,
		MinimumNotional: snapshot.Market.MinimumNotional,
		MaximumLeverage: snapshot.Market.MaximumLeverage,
	}
	return targets, nil
}
