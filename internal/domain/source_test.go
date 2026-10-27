package domain

import (
	"errors"
	"testing"
)

func TestMarketSourceSelectionRejectsCandidateAndUnknownSources(t *testing.T) {
	for _, test := range []struct {
		input string
		want  MarketSource
		live  bool
	}{
		{
			input: " BINANCE ",
			want:  MarketSourceBinance,
			live:  true,
		},
		{
			input: "OKX",
			want:  MarketSourceOKX,
			live:  true,
		},
		{
			input: "bitgit",
			want:  MarketSourceBitget,
		},
		{
			input: "Bybit",
			want:  MarketSourceBybit,
		},
	} {
		source, err := ParseMarketSource(test.input)
		if err != nil || source != test.want {
			t.Fatalf("source %q = %s, %v", test.input, source, err)
		}
		err = source.ValidateSelection()
		if test.live && err != nil || !test.live && !errors.Is(err, ErrMarketSourceUnavailable) {
			t.Fatalf("selection %s = %v", source, err)
		}
	}
	for _, input := range []string{"", "unknown", "binance,okx"} {
		if _, err := ParseMarketSource(input); !errors.Is(err, ErrUnknownMarketSource) {
			t.Fatalf("unknown source %q = %v", input, err)
		}
	}
	if err := MarketSourceUnspecified.ValidateSelection(); !errors.Is(err, ErrUnknownMarketSource) {
		t.Fatalf("zero source selected a default: %v", err)
	}
}
