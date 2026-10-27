package marketworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketfeed"
)

func TestExplicitReplayBuildsBothProviderBooksWithExactMid(t *testing.T) {
	for _, source := range []domain.MarketSource{domain.MarketSourceBinance, domain.MarketSourceOKX} {
		config := configFixture()
		config.Feed.Source = source
		if source == domain.MarketSourceOKX {
			config.Feed.Symbol = "BTC-USDT"
		}
		stream, err := NewReplay(config.Feed, ReplayConfig{
			Bid:             decimal.RequireFromString("99.999999999999999999"),
			Ask:             decimal.NewFromInt(100),
			Quantity:        decimal.NewFromInt(1),
			Interval:        10 * time.Millisecond,
			CallbackTimeout: 100 * time.Millisecond,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		count := 0
		var last int64 = -1
		err = stream.Run(ctx, func(call context.Context, observation marketfeed.Observation) error {
			if _, bounded := call.Deadline(); !bounded {
				t.Error("replay callback lacked a native deadline")
			}
			if observation.State != marketbook.StateReady {
				return nil
			}
			count++
			if observation.Source != source || observation.View.Mid.String() != "99.9999999999999999995" || observation.View.Sequence <= last {
				t.Error("replay lost source, precision or sequence")
			}
			last = observation.View.Sequence
			if count == 3 {
				cancel()
			}
			return nil
		})
		cancel()
		if !errors.Is(err, context.Canceled) || count != 3 {
			t.Fatalf("explicit replay: count=%d, err=%v", count, err)
		}
	}
}

func TestLiveAndReplayFactoriesCannotSubstituteModes(t *testing.T) {
	feed := configFixture().Feed
	if _, err := NewLive(feed, time.Second); err == nil {
		t.Fatal("live adapter accepted replay binding")
	}
	feed.Mode = domain.MarketModeLive
	if _, err := NewReplay(feed, ReplayConfig{}); err == nil {
		t.Fatal("replay adapter accepted live binding")
	}
	if _, err := NewLive(feed, time.Second); err != nil {
		t.Fatal(err)
	}
}
