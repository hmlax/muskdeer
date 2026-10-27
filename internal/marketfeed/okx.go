package marketfeed

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

type subscriptionArgument struct {
	Channel string `json:"channel"`
	Symbol  string `json:"instId"`
}

type subscriptionRequest struct {
	Operation string                 `json:"op"`
	Arguments []subscriptionArgument `json:"args"`
}

func (f *Feed) okx(ctx context.Context, s *socket, book *marketbook.Book, observer Observer, connection int64) error {
	subscription, err := json.Marshal(subscriptionRequest{
		Operation: marketwire.OKXOpSubscribe,
		Arguments: []subscriptionArgument{{
			Channel: marketwire.OKXChannelBooks,
			Symbol:  f.config.Instrument.Symbol,
		}},
	})
	if err != nil {
		return ErrConfig
	}
	if err := s.write(ctx, subscription); err != nil {
		return err
	}
	timer := time.NewTimer(f.config.PingInterval)
	defer timer.Stop()
	waitingPong := false
	subscribed := false
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-timer.C:
			if waitingPong {
				return ErrSession
			}
			if err := s.write(ctx, []byte(marketwire.OKXPing)); err != nil {
				return err
			}
			waitingPong = true
			timer.Reset(f.config.PingInterval)
		case next := <-s.messages:
			s.bytes.Add(-int64(len(next.data)))
			if ctx.Err() != nil {
				return context.Cause(ctx)
			}
			packet, err := marketwire.OKXPacket(ctx, next.data, f.config.Instrument, next.received)
			if err != nil {
				return err
			}
			switch packet.Kind {
			case marketwire.PacketSubscribed:
				if subscribed {
					return marketwire.ErrInvalidPayload
				}
				subscribed = true
			case marketwire.PacketDepth:
				if !subscribed {
					return marketwire.ErrSubscription
				}
				if err := f.apply(ctx, book, packet.Frame, observer, connection); err != nil {
					return err
				}
			case marketwire.PacketPong:
				if !waitingPong {
					return marketwire.ErrInvalidPayload
				}
			}
			waitingPong = false
			timer.Reset(f.config.PingInterval)
		}
	}
}
