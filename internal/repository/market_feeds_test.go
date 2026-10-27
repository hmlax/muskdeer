package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketquality"
)

func TestInvalidMarketBindingsCannotReachDatabase(t *testing.T) {
	store, err := NewMarketFeeds(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	valid := domain.MarketFeedDefinition{
		Source:        domain.MarketSourceBinance,
		InstrumentID:  1,
		Symbol:        "BTCUSDT",
		Mode:          domain.MarketModeLive,
		SourceVersion: domain.MarketSourceVersion,
		Policy:        marketquality.DefaultPolicy(),
	}
	for _, mutate := range []func(*domain.MarketFeedDefinition){
		func(feed *domain.MarketFeedDefinition) { feed.ID = 1 },
		func(feed *domain.MarketFeedDefinition) { feed.Source = domain.MarketSourceBybit },
		func(feed *domain.MarketFeedDefinition) { feed.SourceVersion++ },
		func(feed *domain.MarketFeedDefinition) { feed.Mode = "" },
		func(feed *domain.MarketFeedDefinition) { feed.Symbol = "BTCUSDT?x=1" },
		func(feed *domain.MarketFeedDefinition) { feed.Symbol = "btcusdt" },
		func(feed *domain.MarketFeedDefinition) { feed.InstrumentID = 0 },
		func(feed *domain.MarketFeedDefinition) { feed.Policy.MaximumAge = time.Hour },
		func(feed *domain.MarketFeedDefinition) { feed.Policy.RecoveryWindow = time.Nanosecond },
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := store.Create(ctx, candidate); err == nil {
			t.Fatal("unsafe binding reached database")
		}
	}
	if _, err := store.Get(ctx, 0); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("invalid ID reached database")
	}
	if _, err := store.Create(context.WithoutCancel(ctx), valid); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("unbounded operation reached database")
	}
	cancel()
	if _, err := store.Create(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled binding operation lost cause")
	}
}

func TestTypedMarketPolicyStoragePreservesFractionalBPS(t *testing.T) {
	input := marketquality.DefaultPolicy()
	input.MaximumSpreadBP = decimal.RequireFromString("100.123456789012345678")
	input.MaximumJumpBP = decimal.RequireFromString("200.000000000000000001")
	output, err := marketPolicyValue(marketPolicyModel(input))
	if err != nil || !output.MaximumSpreadBP.Equal(input.MaximumSpreadBP) || !output.MaximumJumpBP.Equal(input.MaximumJumpBP) || output.MaximumAge != input.MaximumAge {
		t.Fatalf("policy lost exact values: %+v, %v", output, err)
	}
	raw := marketPolicyModel(input)
	raw.MaximumJumpBps = "2e2"
	if _, err := marketPolicyValue(raw); err == nil {
		t.Fatal("scientific notation accepted from policy storage")
	}
	raw = marketPolicyModel(input)
	raw.MaximumAgeMs = 9223372036854775807
	if _, err := marketPolicyValue(raw); err == nil {
		t.Fatal("duration multiplication overflow was not rejected")
	}
}
