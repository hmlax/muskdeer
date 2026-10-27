package marketwire

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestTypedBinanceSnapshotAndDepthPreserveAllDecimalDigits(t *testing.T) {
	key := Instrument{
		ID:     1,
		Symbol: "BTCUSDT",
	}
	received := time.UnixMilli(2000)
	snapshot, err := BinanceSnapshot(t.Context(), []byte(`{"lastUpdateId":100,"bids":[["99.123456789012345678","0.000000000000000001"]],"asks":[["101","1"]]}`), key, received)
	if err != nil || snapshot.Source != domain.MarketSourceBinance || !snapshot.Snapshot || snapshot.Sequence != 100 || snapshot.Previous != -1 || !snapshot.Bids[0].Quantity.Equal(decimal.RequireFromString("0.000000000000000001")) {
		t.Fatalf("Binance snapshot = %+v, %v", snapshot, err)
	}
	update, err := BinanceDepth(t.Context(), []byte(`{"e":"depthUpdate","E":1500,"s":"BTCUSDT","U":101,"u":102,"b":[["99.123456789012345678","0"]],"a":[]}`), key, received)
	if err != nil || update.First != 101 || update.Sequence != 102 || update.EventTime.UnixMilli() != 1500 || !update.Bids[0].Quantity.IsZero() || !update.ReceivedAt.Equal(received) {
		t.Fatalf("Binance update = %+v, %v", update, err)
	}
}

func TestTypedOKXUsesSequenceChainAndIgnoresDeprecatedChecksum(t *testing.T) {
	key := Instrument{
		ID:     2,
		Symbol: "BTC-USDT",
	}
	snapshot := `{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"snapshot","data":[{"asks":[["101","1","0","2"]],"bids":[["99","1","0","3"]],"ts":"1500","checksum":0,"prevSeqId":-1,"seqId":0}]}`
	frame, err := OKXDepth(t.Context(), []byte(snapshot), key, time.UnixMilli(2000))
	if err != nil || !frame.Snapshot || frame.Sequence != 0 || frame.Previous != -1 || frame.Source != domain.MarketSourceOKX || frame.EventTime.UnixMilli() != 1500 {
		t.Fatalf("OKX snapshot = %+v, %v", frame, err)
	}
	// The deprecated CRC field cannot determine whether the sequence is valid.
	if _, err := OKXDepth(t.Context(), []byte(strings.Replace(snapshot, `"checksum":0`, `"checksum":123`, 1)), key, time.UnixMilli(2000)); err != nil {
		t.Fatalf("deprecated CRC still controlled integrity: %v", err)
	}
}

func TestMarketJSONRejectsNumbersDuplicateKeysAliasesAndBadTupleShapes(t *testing.T) {
	key := Instrument{
		ID:     1,
		Symbol: "BTCUSDT",
	}
	valid := `{"e":"depthUpdate","E":1500,"s":"BTCUSDT","U":101,"u":102,"b":[["99","1"]],"a":[]}`
	for _, data := range []string{
		strings.Replace(valid, `"99"`, `99`, 1),
		strings.Replace(valid, `"99"`, `"1e2"`, 1),
		strings.Replace(valid, `"1"`, `"NaN"`, 1),
		strings.Replace(valid, `"u":102`, `"u":102,"u":103`, 1),
		strings.Replace(valid, `"s":"BTCUSDT"`, `"S":"BTCUSDT"`, 1),
		strings.Replace(valid, `"s":"BTCUSDT"`, `"s":"ETHUSDT"`, 1),
		strings.Replace(valid, `["99","1"]`, `["99","1","extra"]`, 1),
		strings.Replace(valid, `"a":[]`, `"a":null`, 1),
		strings.Replace(valid, `"U":101`, `"U":103`, 1),
		valid + ` {}`,
	} {
		if _, err := BinanceDepth(t.Context(), []byte(data), key, time.UnixMilli(2000)); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("malformed market payload accepted: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := BinanceDepth(ctx, []byte(valid), key, time.UnixMilli(2000)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled market parser lost its cause: %v", err)
	}
}

func TestOKXRejectsAmbiguousEnvelopeAndMissingSequence(t *testing.T) {
	key := Instrument{
		ID:     2,
		Symbol: "BTC-USDT",
	}
	valid := `{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"update","data":[{"asks":[],"bids":[["99","0","0","0"]],"ts":"1500","checksum":0,"prevSeqId":1,"seqId":2}]}`
	for _, data := range []string{
		strings.Replace(valid, `"channel":"books"`, `"channel":"books5"`, 1),
		strings.Replace(valid, `"instId":"BTC-USDT"`, `"instId":"BTC-USDT","instId":"ETH-USDT"`, 1),
		strings.Replace(valid, `"seqId":2`, `"seqID":2`, 1),
		strings.Replace(valid, `"seqId":2`, `"seqId":null`, 1),
		strings.Replace(valid, `"prevSeqId":1`, `"prevSeqId":-1`, 1),
		strings.Replace(valid, `"ts":"1500"`, `"ts":"1.5e3"`, 1),
		strings.Replace(valid, `["99","0","0","0"]`, `[99,"0","0","0"]`, 1),
		strings.Replace(valid, `["99","0","0","0"]`, `["99","0","0","-1"]`, 1),
		strings.Replace(valid, `"asks":[]`, `"asks":{}`, 1),
	} {
		if _, err := OKXDepth(t.Context(), []byte(data), key, time.UnixMilli(2000)); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("malformed OKX update accepted: %v", err)
		}
	}
	if frame, err := OKXDepth(t.Context(), []byte(valid), key, time.UnixMilli(2000)); err != nil || len(frame.Bids) != 1 || !frame.Bids[0].Quantity.IsZero() {
		t.Fatalf("valid zero-sized update was lost: %+v, %v", frame, err)
	}
}

func FuzzProviderPayloadsStayTypedAndBounded(f *testing.F) {
	f.Add(uint8(0), []byte(`{"lastUpdateId":1,"bids":[["99","1"]],"asks":[["101","1"]]}`))
	f.Add(uint8(1), []byte(`{"e":"depthUpdate","E":1500,"s":"BTCUSDT","U":1,"u":2,"b":[],"a":[]}`))
	f.Add(uint8(2), []byte(`{"arg":{"channel":"books","instId":"BTC-USDT"},"action":"update","data":[{"asks":[],"bids":[],"ts":"1500","prevSeqId":1,"seqId":1}]}`))
	f.Fuzz(func(t *testing.T, provider uint8, data []byte) {
		key := Instrument{
			ID:     1,
			Symbol: "BTCUSDT",
		}
		received := time.UnixMilli(2000)
		switch provider % 3 {
		case 0:
			_, _ = BinanceSnapshot(t.Context(), data, key, received)
		case 1:
			_, _ = BinanceDepth(t.Context(), data, key, received)
		case 2:
			key.Symbol = "BTC-USDT"
			_, _ = OKXDepth(t.Context(), data, key, received)
		}
	})
}
