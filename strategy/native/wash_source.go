package native

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/hmlax/muskdeer/deer/logging"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	values "github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

const washSourceStatsMessage = "WASH_SOURCE_STATS"

type washSourceStats struct {
	received  int64
	duplicate int64
	invalid   int64
	tooSmall  int64
	submitted int64
	published int64
	unknown   int64
	failed    int64
	last      int64
}

func followWashSource(ctx context.Context, cfg *venueconfig.RuntimeConfig, symbol string, updates <-chan *washObservation, failure <-chan error, send func(context.Context, *pb.WashTradeRequest) error, logger *logging.Logger) error {
	stats := &washSourceStats{}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-failure:
			return err
		case observation := <-updates:
			publishWashSource(ctx, cfg, symbol, observation, send, stats)
		case <-ticker.C:
			if err := logger.LogAttrs(ctx, slog.LevelInfo, washSourceStatsMessage,
				slog.String("symbol", symbol), slog.Int64("received", stats.received),
				slog.Int64("duplicate", stats.duplicate), slog.Int64("invalid", stats.invalid),
				slog.Int64("too_small", stats.tooSmall), slog.Int64("submitted", stats.submitted),
				slog.Int64("published", stats.published), slog.Int64("unknown", stats.unknown), slog.Int64("failed", stats.failed)); err != nil {
				return err
			}
		}
	}
}

// Each source identity is consumed once before calling the SDK. A lost reply
// never retries it; later events retain their own original freshness deadline.
func publishWashSource(ctx context.Context, cfg *venueconfig.RuntimeConfig, symbol string, observation *washObservation, send func(context.Context, *pb.WashTradeRequest) error, stats *washSourceStats) {
	stats.received++
	trade := observation.trade
	id, err := values.TradeSequence(trade)
	if err != nil {
		stats.invalid++
		return
	}
	if id <= stats.last {
		stats.duplicate++
		return
	}
	if values.ValidateTrade(trade, symbol, time.Now()) != nil || trade.GetPublisher().GetAccountId() != values.TradeKey(symbol) {
		stats.invalid++
		return
	}
	stats.last = id
	input := &pb.WashTradeRequest{
		StrategyInstanceId: cfg.InstanceID,
		StrategyRevision:   cfg.Revision,
		Symbol:             symbol,
		Source:             &pb.WashTradeRequest_Trade{Trade: trade},
	}
	target, err := values.WashTarget(cfg.Venue, input)
	if err != nil {
		stats.tooSmall++
		return
	}
	input.Side, input.Price, input.Quantity = target.Side, target.Price, target.Quantity
	op, cancel := context.WithTimeout(ctx, min(cfg.OperationTimeout, time.Until(time.UnixMilli(trade.ValidUntilMs))))
	defer cancel()
	if op.Err() != nil {
		stats.invalid++
		return
	}
	stats.submitted++
	err = send(op, input)
	switch {
	case err == nil:
		stats.published++
	case errors.Is(err, domain.ErrOutcomeUnknown):
		stats.unknown++
	default:
		stats.failed++
	}
}
