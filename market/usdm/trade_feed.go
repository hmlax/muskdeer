package usdm

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

const (
	aggregateTradeEvent    = "aggTrade"
	aggregateTradeEndpoint = "wss://fstream.binance.com/market/ws/"
)

type tradeFrame struct {
	Event      string `json:"e"`
	EventAt    int64  `json:"E"`
	Symbol     string `json:"s"`
	ID         int64  `json:"a"`
	Price      string `json:"p"`
	Quantity   string `json:"q"`
	First      int64  `json:"f"`
	Last       int64  `json:"l"`
	TradeAt    int64  `json:"T"`
	BuyerMaker *bool  `json:"m"`
	SymbolType *int32 `json:"st"`
}

// DecodeTrade maps buyer-is-maker to an aggressive sell and rejects stale,
// malformed and foreign-symbol observations without borrowing book prices.
func DecodeTrade(data []byte, symbol string, received time.Time) (*commonv1.HimalayaTrade, error) {
	var frame tradeFrame
	if len(data) > 64*1024 || json.Unmarshal(data, &frame) != nil || frame.Event != aggregateTradeEvent ||
		frame.ID <= 0 || frame.First <= 0 || frame.Last < frame.First || frame.BuyerMaker == nil ||
		frame.SymbolType != nil && *frame.SymbolType != 1 {
		return nil, native.ErrInvalid
	}
	side := domain.SideBuy
	if *frame.BuyerMaker {
		side = domain.SideSell
	}
	trade := &commonv1.HimalayaTrade{
		Symbol:       frame.Symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		EventId:      strconv.FormatInt(frame.ID, 10),
		Side:         string(side),
		Price:        frame.Price,
		Quantity:     frame.Quantity,
		EventAtMs:    frame.EventAt,
		TradeAtMs:    frame.TradeAt,
		ReceivedAtMs: received.UnixMilli(),
		ValidUntilMs: min(frame.EventAt, frame.TradeAt, received.UnixMilli()) + native.TradeAge.Milliseconds(),
	}
	if err := native.ValidateTrade(trade, symbol, received); err != nil {
		return nil, err
	}
	return trade, nil
}

// TradeFeed owns an independent aggregate-trade socket using the shared bounded
// session lifecycle. Depth updates remain on their original public endpoint.
type TradeFeed struct{ session *Feed }

// NewTradeFeed creates no connection or goroutine.
func NewTradeFeed(symbol string) (*TradeFeed, error) {
	feed, err := NewFeed(symbol)
	if err != nil {
		return nil, err
	}
	feed.url = aggregateTradeEndpoint + strings.ToLower(symbol) + "@" + aggregateTradeEvent
	return &TradeFeed{session: feed}, nil
}

// Run publishes only strictly increasing aggregate IDs within an owned session.
func (f *TradeFeed) Run(ctx context.Context, publish func(context.Context, *commonv1.HimalayaTrade) error) error {
	if f == nil || f.session == nil || ctx == nil || publish == nil {
		return native.ErrInvalid
	}
	var sequence int64
	return f.session.runFrames(ctx, func(work context.Context, data []byte, received time.Time) error {
		trade, err := DecodeTrade(data, f.session.symbol, received)
		if err != nil {
			return err
		}
		id, err := native.TradeSequence(trade)
		if err != nil || id <= sequence {
			return native.ErrInvalid
		}
		sequence = id
		return publish(work, trade)
	})
}
