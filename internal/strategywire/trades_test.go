package strategywire

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestTradeWireKeepsEighteenPlacePricesAndRequiresOriginalPageBindings(t *testing.T) {
	value := domain.AdminTrade{
		ID:             9007199254740993,
		ExecutionVenue: domain.ExecutionVenueSimulator,
		TradeID:        "wire-precise-trade",
		StrategyID:     9007199254740995,
		AccountID:      "wire-trade-account",
		InstrumentID:   1,
		ConfigVersion:  2,
		Source:         domain.MarketSourceOKX,
		CoreOrderID:    9007199254740996,
		ClientOrderID:  "wire-trade-client",
		Side:           domain.SideSell,
		Price:          decimal.RequireFromString("100.000000000000000001"),
		Quantity:       decimal.NewFromInt(2),
		QuoteNotional:  decimal.RequireFromString("200.000000000000000002"),
		FeeAmount:      decimal.Zero,
		FeeAsset:       "USDT",
		RecordedAt:     time.UnixMilli(1700000000000),
	}
	trade, err := TradeProto(value)
	if err != nil || trade.Price != "100.000000000000000001" || trade.QuoteNotional != "200.000000000000000002" || trade.FeeAmount != "0" || trade.OrderId != nil || trade.PortfolioFact != nil {
		t.Fatal("unlinked authoritative fill lost exact decimals or presence", err)
	}
	filter := domain.AdminTradeFilter{
		StrategyID: value.StrategyID,
		Page: domain.AdminPage{
			AfterID: value.ID - 1,
			Limit:   1,
		},
		Side: domain.SideSell,
	}
	page := &strategyv1.ListStrategyTradesReply{
		Trades:      []*strategyv1.StrategyTrade{trade},
		NextAfterId: trade.Id,
	}
	if !ValidTradePage(page, filter) {
		t.Fatal("exact core fill page rejected")
	}
	for _, change := range []func(*strategyv1.ListStrategyTradesReply){
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].Id = filter.Page.AfterID },
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].Side = string(domain.SideBuy) },
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].ExecutionVenue = "other" },
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].Price = "1e20" },
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].FeeAmount = "200.000000000000000002" },
		func(p *strategyv1.ListStrategyTradesReply) { p.Trades[0].ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(p *strategyv1.ListStrategyTradesReply) { p.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(p *strategyv1.ListStrategyTradesReply) { p.NextAfterId++ },
	} {
		candidate := proto.CloneOf(page)
		change(candidate)
		if ValidTradePage(candidate, filter) {
			t.Fatal("malformed or filter-mismatched trade page accepted")
		}
	}
}
