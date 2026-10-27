package marketwire

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/hmlax/muskdeer/internal/marketbook"
)

// PacketKind separates subscription control from order book observations.
type PacketKind string

// Typed packet variants and fixed OKX control protocol values.
const (
	PacketDepth         PacketKind = "DEPTH"
	PacketSubscribed    PacketKind = "SUBSCRIBED"
	PacketPong          PacketKind = "PONG"
	OKXOpSubscribe                 = "subscribe"
	OKXEventError                  = "error"
	OKXEventNotice                 = "notice"
	OKXRestartCode                 = "64008"
	OKXPing                        = "ping"
	OKXPong                        = "pong"
	MessageSubscription            = "market subscription was rejected or interrupted"
)

// ErrSubscription never includes the provider's untrusted message text.
var ErrSubscription = errors.New(MessageSubscription)

// Packet has one concrete book variant; control packets cannot refresh prices.
type Packet struct {
	Kind  PacketKind
	Frame marketbook.Frame
}

type okxEnvelope struct {
	Event    string       `json:"event"`
	Code     string       `json:"code"`
	Message  string       `json:"msg"`
	ConnID   string       `json:"connId"`
	ID       string       `json:"id"`
	Argument *okxArgument `json:"arg"`
	Action   string       `json:"action"`
	Data     []okxBook    `json:"data"`
}

// OKXPacket checks an exact typed union before selecting control or book data.
func OKXPacket(ctx context.Context, data []byte, instrument Instrument, received time.Time) (Packet, error) {
	if ctx == nil || !validScope(instrument, received) {
		return Packet{}, ErrInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return Packet{}, err
	}
	if bytes.Equal(data, []byte(OKXPong)) {
		return Packet{Kind: PacketPong}, nil
	}
	raw, err := decode[okxEnvelope](ctx, data)
	if err != nil {
		return Packet{}, err
	}
	if raw.Event == "" {
		if raw.Code != "" || raw.Message != "" || raw.ConnID != "" || raw.ID != "" {
			return Packet{}, ErrInvalidPayload
		}
		frame, err := okxFrame(ctx, okxDepth{
			Argument: raw.Argument,
			Action:   raw.Action,
			Data:     raw.Data,
		}, instrument, received)
		return Packet{
			Kind:  PacketDepth,
			Frame: frame,
		}, err
	}
	if raw.Action != "" || raw.Data != nil || len(raw.ConnID) > 128 || len(raw.ID) > 64 || len(raw.Message) > 1024 {
		return Packet{}, ErrInvalidPayload
	}
	switch raw.Event {
	case OKXOpSubscribe:
		if raw.Argument == nil || raw.Argument.Channel != OKXChannelBooks || raw.Argument.Instrument != instrument.Symbol || raw.Code != "" || raw.Message != "" || raw.ConnID == "" {
			return Packet{}, ErrInvalidPayload
		}
		return Packet{Kind: PacketSubscribed}, nil
	case OKXEventError:
		if raw.Code == "" {
			return Packet{}, ErrInvalidPayload
		}
		return Packet{}, ErrSubscription
	case OKXEventNotice:
		if raw.Code != OKXRestartCode {
			return Packet{}, ErrInvalidPayload
		}
		return Packet{}, ErrSubscription
	default:
		return Packet{}, ErrInvalidPayload
	}
}
