package marketbook

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestDeletingPastSnapshotCoverageCannotExposeUnknownTop(t *testing.T) {
	for _, side := range []string{"bid", "ask"} {
		t.Run(side, func(t *testing.T) {
			book, err := New(domain.MarketSourceOKX, 1, "BTC-USDT", MaximumLevels)
			if err != nil {
				t.Fatal(err)
			}
			frame := Frame{
				Source:       domain.MarketSourceOKX,
				InstrumentID: 1,
				Symbol:       "BTC-USDT",
				Snapshot:     true,
				Previous:     -1,
				Sequence:     1,
				Bids: []Level{{
					Price:    decimal.RequireFromString("99"),
					Quantity: decimal.NewFromInt(1),
				}},
				Asks: []Level{{
					Price:    decimal.RequireFromString("101"),
					Quantity: decimal.NewFromInt(1),
				}},
				EventTime:  time.UnixMilli(1000),
				ReceivedAt: time.UnixMilli(1001),
			}
			if err := book.Apply(frame); err != nil {
				t.Fatal(err)
			}
			frame.Snapshot, frame.Previous, frame.Sequence = false, 1, 2
			frame.Bids, frame.Asks = nil, nil
			if side == "bid" {
				frame.Bids = []Level{
					{Price: decimal.NewFromInt(99)},
					{
						Price:    decimal.NewFromInt(97),
						Quantity: decimal.NewFromInt(1),
					},
				}
			} else {
				frame.Asks = []Level{
					{Price: decimal.NewFromInt(101)},
					{
						Price:    decimal.NewFromInt(103),
						Quantity: decimal.NewFromInt(1),
					},
				}
			}
			// The omitted snapshot depth might contain a better price (98/102).
			if err := book.Apply(frame); !errors.Is(err, ErrInvalidBook) {
				t.Fatalf("unknown depth was assumed empty: %v", err)
			}
			if _, err := book.Top(); !errors.Is(err, ErrNotReady) {
				t.Fatal("book exposed a price beyond snapshot coverage")
			}
		})
	}
}

func TestBinanceBridgesSnapshotIgnoresOldDiffAndInvalidatesGap(t *testing.T) {
	book := newBookFixture(t, domain.MarketSourceBinance)
	snapshot := frameFixture(domain.MarketSourceBinance, true, 100, -1, 0)
	if err := book.Apply(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Top(); !errors.Is(err, ErrNotReady) {
		t.Fatal("unbridged REST snapshot drove a quote")
	}
	old := frameFixture(domain.MarketSourceBinance, false, 100, 0, 99)
	if err := book.Apply(old); err != nil {
		t.Fatal(err)
	}
	update := frameFixture(domain.MarketSourceBinance, false, 103, 0, 100)
	update.Bids = []Level{levelFixture("99.123456789012345678", "2")}
	if err := book.Apply(update); err != nil {
		t.Fatal(err)
	}
	top, err := book.Top()
	if err != nil || top.Sequence != 103 || !top.Bid.Price.Equal(decimal.RequireFromString("99.123456789012345678")) || !top.Bid.Quantity.Equal(decimal.NewFromInt(2)) || !top.Mid.Equal(decimal.RequireFromString("100.061728394506172839")) {
		t.Fatalf("bridged book changed exact values: %+v, %v", top, err)
	}
	if err := book.Apply(old); err != nil {
		t.Fatal(err)
	}
	if after, err := book.Top(); err != nil || !after.Bid.Price.Equal(top.Bid.Price) || after.Sequence != top.Sequence {
		t.Fatalf("old update regressed the book: %+v, %v", after, err)
	}
	gap := frameFixture(domain.MarketSourceBinance, false, 106, 0, 105)
	if err := book.Apply(gap); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("gap accepted: %v", err)
	}
	if _, err := book.Top(); !errors.Is(err, ErrNotReady) {
		t.Fatal("invalid book still supplied prices")
	}
	if err := book.Apply(frameFixture(domain.MarketSourceBinance, true, 110, -1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := book.Apply(frameFixture(domain.MarketSourceBinance, false, 111, 0, 111)); err != nil {
		t.Fatal(err)
	}
	if recovered, err := book.Top(); err != nil || recovered.Generation <= top.Generation || recovered.Sequence != 111 {
		t.Fatalf("snapshot recovery did not replace invalid epoch: %+v, %v", recovered, err)
	}
}

func TestOKXKeepaliveDuplicatesAndMaintenanceReset(t *testing.T) {
	book := newBookFixture(t, domain.MarketSourceOKX)
	if err := book.Apply(frameFixture(domain.MarketSourceOKX, true, 10, -1, 0)); err != nil {
		t.Fatal(err)
	}
	update := frameFixture(domain.MarketSourceOKX, false, 15, 10, 0)
	update.Bids = []Level{levelFixture("99", "3")}
	if err := book.Apply(update); err != nil {
		t.Fatal(err)
	}
	before, err := book.Top()
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Apply(update); err != nil {
		t.Fatalf("exact repeated update was rejected: %v", err)
	}
	heartbeat := frameFixture(domain.MarketSourceOKX, false, 15, 15, 0)
	heartbeat.Bids, heartbeat.Asks = nil, nil
	heartbeat.EventTime = heartbeat.EventTime.Add(time.Second)
	heartbeat.ReceivedAt = heartbeat.ReceivedAt.Add(time.Second)
	if err := book.Apply(heartbeat); err != nil {
		t.Fatal(err)
	}
	reset := frameFixture(domain.MarketSourceOKX, false, 3, 15, 0)
	reset.EventTime = heartbeat.EventTime.Add(time.Second)
	reset.ReceivedAt = heartbeat.ReceivedAt.Add(time.Second)
	reset.Bids, reset.Asks = nil, nil
	if err := book.Apply(reset); err != nil {
		t.Fatalf("documented maintenance reset rejected: %v", err)
	}
	after, err := book.Top()
	if err != nil || after.Sequence != 3 || after.Generation <= before.Generation || !after.Bid.Quantity.Equal(before.Bid.Quantity) {
		t.Fatalf("maintenance reset changed levels or lost generation: %+v, %v", after, err)
	}
	if err := book.Apply(frameFixture(domain.MarketSourceOKX, false, 4, 2, 0)); !errors.Is(err, ErrSequenceGap) {
		t.Fatalf("OKX wrong predecessor accepted: %v", err)
	}
}

func TestSourcesAreIndependentAndMalformedLevelsNeverPublish(t *testing.T) {
	binance := newBookFixture(t, domain.MarketSourceBinance)
	okx := newBookFixture(t, domain.MarketSourceOKX)
	snapshot := frameFixture(domain.MarketSourceOKX, true, 1, -1, 0)
	if err := okx.Apply(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := binance.Apply(snapshot); !errors.Is(err, ErrIdentity) {
		t.Fatalf("provider crossed into another book: %v", err)
	}
	bad := frameFixture(domain.MarketSourceBinance, true, 1, -1, 0)
	bad.Bids = append(bad.Bids, levelFixture("99.0", "2"))
	if err := binance.Apply(bad); !errors.Is(err, ErrInvalidBook) {
		t.Fatalf("normalized duplicate price accepted: %v", err)
	}
	if _, err := okx.Top(); err != nil {
		t.Fatalf("other provider failure invalidated healthy source: %v", err)
	}
	bad = frameFixture(domain.MarketSourceOKX, false, 2, 1, 0)
	bad.Bids = []Level{levelFixture("102", "1")}
	if err := okx.Apply(bad); !errors.Is(err, ErrInvalidBook) {
		t.Fatalf("crossed book accepted: %v", err)
	}
	if _, err := okx.Top(); !errors.Is(err, ErrNotReady) {
		t.Fatal("crossed book supplied a price")
	}
}

func newBookFixture(t *testing.T, source domain.MarketSource) *Book {
	t.Helper()
	book, err := New(source, 1, "BTCUSDT", 5000)
	if err != nil {
		t.Fatal(err)
	}
	return book
}

func TestZeroQuantityDeletesWithoutRetainingCallerMemory(t *testing.T) {
	book := newBookFixture(t, domain.MarketSourceOKX)
	snapshot := frameFixture(domain.MarketSourceOKX, true, 1, -1, 0)
	snapshot.Bids = append(snapshot.Bids, levelFixture("98", "4"))
	if err := book.Apply(snapshot); err != nil {
		t.Fatal(err)
	}
	snapshot.Bids[0].Quantity = decimal.NewFromInt(999)
	before, err := book.Top()
	if err != nil || !before.Bid.Quantity.Equal(decimal.NewFromInt(1)) {
		t.Fatalf("book retained caller depth memory: %+v, %v", before, err)
	}
	update := frameFixture(domain.MarketSourceOKX, false, 2, 1, 0)
	update.Bids = []Level{
		levelFixture("99", "0"),
		levelFixture("97", "0"),
	}
	update.Asks = nil
	if err := book.Apply(update); err != nil {
		t.Fatal(err)
	}
	after, err := book.Top()
	if err != nil || !after.Bid.Price.Equal(decimal.NewFromInt(98)) || !after.Bid.Quantity.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("zero quantity did not remove a level exactly: %+v, %v", after, err)
	}
}

func TestMidRetainsHalfOfSmallestPriceUnitAndCapacityFailsClosed(t *testing.T) {
	book, err := New(domain.MarketSourceOKX, 1, "BTCUSDT", 1)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := frameFixture(domain.MarketSourceOKX, true, 0, -1, 0)
	snapshot.Bids = []Level{levelFixture("99.123456789012345679", "1")}
	if err := book.Apply(snapshot); err != nil {
		t.Fatal(err)
	}
	top, err := book.Top()
	if err != nil || top.Mid.String() != "100.0617283945061728395" {
		t.Fatalf("reference price lost its nineteenth place: %+v, %v", top, err)
	}
	update := frameFixture(domain.MarketSourceOKX, false, 1, 0, 0)
	update.Bids = []Level{levelFixture("98", "1")}
	update.Asks = nil
	if err := book.Apply(update); !errors.Is(err, ErrInvalidBook) {
		t.Fatalf("capacity silently truncated the book: %v", err)
	}
	if _, err := book.Top(); !errors.Is(err, ErrNotReady) {
		t.Fatal("overflowed book remained priceable")
	}
}

func frameFixture(source domain.MarketSource, snapshot bool, sequence, previous, first int64) Frame {
	return Frame{
		Source:       source,
		InstrumentID: 1,
		Symbol:       "BTCUSDT",
		Snapshot:     snapshot,
		Sequence:     sequence,
		Previous:     previous,
		First:        first,
		Bids:         []Level{levelFixture("99", "1")},
		Asks:         []Level{levelFixture("101", "1")},
		EventTime:    time.UnixMilli(1000).UTC(),
		ReceivedAt:   time.UnixMilli(1000).UTC(),
	}
}

func levelFixture(price, quantity string) Level {
	return Level{
		Price:    decimal.RequireFromString(price),
		Quantity: decimal.RequireFromString(quantity),
	}
}
