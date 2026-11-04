package usdm

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	marketvenuev1 "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

const tradeWorkerPrefix = "market.usdm.trade."

// GetTrade returns the latest owned aggregate observation; it never falls back
// to depth prices or another exchange when the trade stream is unavailable.
func (r *Runtime) GetTrade(ctx context.Context, request *marketvenuev1.TradeRequest) (*commonv1.VenueTrade, error) {
	if !rpcsecurity.HasService(ctx, rpcsecurity.ServiceStrategy) && !rpcsecurity.HasService(ctx, rpcsecurity.ServiceExecution) {
		return nil, status.Error(codes.PermissionDenied, native.MessageInvalid)
	}
	r.mu.RLock()
	trade := proto.CloneOf(r.trades[request.GetSymbol()])
	r.mu.RUnlock()
	if trade == nil || native.ValidateTrade(trade, request.GetSymbol(), time.Now()) != nil || repository.CheckNativeAuthority(ctx, r.db, trade.Publisher) != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	return trade, nil
}

func (r *Runtime) runTrades(ctx context.Context, symbol string) error {
	for ctx.Err() == nil {
		if err := r.tradeSession(ctx, symbol); err != nil && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("symbol", symbol), slog.String("stream", aggregateTradeEvent)); logErr != nil {
				return logErr
			}
		}
		r.mu.Lock()
		delete(r.trades, symbol)
		r.mu.Unlock()
		if err := native.Wait(ctx, time.Second); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (r *Runtime) tradeSession(ctx context.Context, symbol string) error {
	op, cancel := context.WithTimeout(ctx, r.cfg.OperationTimeout)
	token, err := r.leases.Acquire(op, domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: native.TradeKey(symbol),
	}, r.owner, r.cfg.Venue.LeaseTtl.AsDuration())
	cancel()
	if err != nil {
		return err
	}
	feed, err := NewTradeFeed(symbol)
	if err != nil {
		return err
	}
	return feed.Run(ctx, func(work context.Context, trade *commonv1.VenueTrade) error {
		if time.Until(token.ExpiresAt) < r.cfg.Venue.LeaseTtl.AsDuration()/2 {
			bounded, cancel := context.WithTimeout(work, r.cfg.OperationTimeout)
			defer cancel()
			token, err = r.leases.Renew(bounded, token, r.cfg.Venue.LeaseTtl.AsDuration())
			if err != nil {
				return err
			}
		}
		if err := work.Err(); err != nil {
			return err
		}
		trade.Publisher = native.Authority(token)
		trade.ValidUntilMs = min(trade.ValidUntilMs, token.ExpiresAt.UnixMilli()-50)
		if native.ValidateTrade(trade, symbol, time.Now()) != nil {
			return native.ErrInvalid
		}
		r.mu.Lock()
		r.trades[symbol] = proto.CloneOf(trade)
		r.mu.Unlock()
		return nil
	})
}
