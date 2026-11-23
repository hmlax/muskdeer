package native

import (
	"strings"
	"testing"
)

func TestWorkerSymbolPreservesIdentityAndFitsWorkerNames(t *testing.T) {
	for _, symbol := range []string{"BTCUSDT", "akeusdt", "1000PEPEUSDT"} {
		if WorkerSymbol(symbol) != symbol {
			t.Errorf("ASCII worker label changed: %q", symbol)
		}
	}
	for _, symbol := range []string{"币安人生USDT", strings.Repeat("币", 8)} {
		label := WorkerSymbol(symbol)
		if label != WorkerSymbol(symbol) || label == symbol || len(label) > 25 {
			t.Fatalf("unstable or unbounded label: %q", label)
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				t.Fatalf("unsafe label: %q", label)
			}
		}
		if len(SyntheticWorker+label+".9223372036854775807") > 64 {
			t.Fatal("worker name exceeds lifecycle bound")
		}
	}
	if WorkerSymbol("币安人生USDT") == WorkerSymbol("币安人生USDC") {
		t.Fatal("different catalog symbols share a worker label")
	}
}
