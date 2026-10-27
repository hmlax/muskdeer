package main

import (
	"bytes"
	"testing"
)

func TestProbeRejectsUnsafeOrMissingSelectionWithoutNetworking(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"-source", "bitget", "-symbol", "BTCUSDT"},
		{"-source", "binance", "-symbol", "BTCUSDT?x=1"},
		{"-source", "okx", "-symbol", "BTC-USDT", "-duration", "1h"},
		{"-source", "okx", "-symbol", "BTC-USDT", "-reconnect-after", "30s"},
	} {
		var output, diagnostic bytes.Buffer
		if code := run(t.Context(), args, &output, &diagnostic); code != 2 || output.Len() != 0 {
			t.Fatalf("invalid probe configuration started network work: code=%d", code)
		}
	}
}
