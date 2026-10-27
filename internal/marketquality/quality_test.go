package marketquality

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/marketbook"
	"github.com/hmlax/muskdeer/internal/marketwire"
)

func TestQualityRequiresFreshIndependentRecoverySamplesAndExpiresWithoutMessages(t *testing.T) {
	tracker := fixture(t, domain.MarketSourceBinance)
	now := time.Unix(1800000000, 0)
	view := sample(domain.MarketSourceBinance, 1, "100", now)
	reading, err := tracker.Observe(view, 1, now)
	if err != nil || reading.Quality != domain.MarketQualityWarming || !reading.Mid.IsZero() {
		t.Fatalf("first sample authorized a quote: %+v, %v", reading, err)
	}
	for index := range 4 {
		reading, err = tracker.Observe(view, 1, now.Add(time.Duration(index)*50*time.Millisecond))
		if err != nil || reading.Quality != domain.MarketQualityWarming || !reading.Mid.IsZero() {
			t.Fatal("duplicate sample completed recovery")
		}
	}
	for index := int64(2); index <= 3; index++ {
		at := now.Add(time.Duration(index) * 100 * time.Millisecond)
		reading, err = tracker.Observe(sample(domain.MarketSourceBinance, index, "100", at), 1, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reading.Quality != domain.MarketQualityHealthy || reading.Mid.String() != "100" || reading.Selected(domain.MarketSourceBinance, 1, "BTCUSDT", domain.MarketModeReplay, reading.EvaluatedAt, 2*time.Second) != nil {
		t.Fatalf("complete healthy samples rejected: %+v", reading)
	}
	expired := tracker.Current(reading.ValidUntil)
	if expired.Quality != domain.MarketQualityStale || !expired.Mid.IsZero() || expired.Selected(domain.MarketSourceBinance, 1, "BTCUSDT", domain.MarketModeReplay, reading.ValidUntil, 2*time.Second) == nil {
		t.Fatal("silence kept a healthy executable reference")
	}
}

func TestOutlierDoesNotPoisonBaselineOrAnotherSourceAndReconnectDoesNotResetIt(t *testing.T) {
	binance, okx := fixture(t, domain.MarketSourceBinance), fixture(t, domain.MarketSourceOKX)
	now := time.Unix(1800000000, 0)
	warm(t, binance, domain.MarketSourceBinance, now)
	warm(t, okx, domain.MarketSourceOKX, now)
	at := now.Add(time.Second)
	reading, err := binance.Observe(sample(domain.MarketSourceBinance, 1, "200", at), 2, at)
	if err != nil || reading.Quality != domain.MarketQualityOutlier || !reading.Mid.IsZero() {
		t.Fatalf("reconnection bypassed outlier baseline: %+v, %v", reading, err)
	}
	if other := okx.Current(at); other.Quality != domain.MarketQualityHealthy || other.Mid.String() != "100" {
		t.Fatal("one source poisoned the other")
	}
	for index := int64(2); index <= 4; index++ {
		at = now.Add(time.Second + time.Duration(index)*100*time.Millisecond)
		reading, err = binance.Observe(sample(domain.MarketSourceBinance, index, "100", at), 2, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reading.Quality != domain.MarketQualityHealthy || reading.Mid.String() != "100" {
		t.Fatalf("outlier replaced accepted baseline: %+v", reading)
	}
	if err := reading.Selected(domain.MarketSourceOKX, 1, "BTCUSDT", domain.MarketModeReplay, at, time.Second); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("instance selection admitted an unselected source")
	}
	if err := reading.Selected(domain.MarketSourceBinance, 1, "BTCUSDT", domain.MarketModeLive, at, time.Second); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("replay was accepted as live")
	}
}

func TestInitialTransientOutlierCanRecoverButCannotAuthorizeFirstSample(t *testing.T) {
	tracker := fixture(t, domain.MarketSourceBinance)
	now := time.Unix(1800000000, 0)
	if reading, err := tracker.Observe(sample(domain.MarketSourceBinance, 1, "200", now), 1, now); err != nil || !reading.Mid.IsZero() {
		t.Fatal("initial price skipped warmup")
	}
	var reading domain.MarketReading
	for index := int64(2); index <= 4; index++ {
		at := now.Add(time.Duration(index) * 100 * time.Millisecond)
		var err error
		reading, err = tracker.Observe(sample(domain.MarketSourceBinance, index, "100", at), 1, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reading.Quality != domain.MarketQualityHealthy || reading.Mid.String() != "100" {
		t.Fatal("unaccepted bootstrap candidate poisoned baseline permanently")
	}
}

func TestQualityRejectsQueueAgeClockSpreadAndMalformedValues(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, test := range []struct {
		name    string
		mutate  func(*marketbook.View)
		quality domain.MarketQuality
	}{
		{
			name: "queued",
			mutate: func(view *marketbook.View) {
				view.EventTime, view.ReceivedAt = now.Add(-3*time.Second), now.Add(-3*time.Second)
			},
			quality: domain.MarketQualityStale,
		},
		{
			name: "old exchange event with fresh reception",
			mutate: func(view *marketbook.View) {
				view.EventTime = now.Add(-3 * time.Second)
			},
			quality: domain.MarketQualityStale,
		},
		{
			name: "future",
			mutate: func(view *marketbook.View) {
				view.EventTime = now.Add(time.Second)
			},
			quality: domain.MarketQualityClock,
		},
		{
			name: "spread",
			mutate: func(view *marketbook.View) {
				view.Bid.Price, view.Ask.Price = decimal.NewFromInt(90), decimal.NewFromInt(110)
			},
			quality: domain.MarketQualitySpread,
		},
		{
			name: "wrong mid",
			mutate: func(view *marketbook.View) {
				view.Mid = decimal.NewFromInt(90)
			},
			quality: domain.MarketQualityInvalid,
		},
		{
			name: "hostile exponent",
			mutate: func(view *marketbook.View) {
				view.Bid.Price = decimal.New(1, 2000000000)
			},
			quality: domain.MarketQualityInvalid,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracker := fixture(t, domain.MarketSourceBinance)
			view := sample(domain.MarketSourceBinance, 1, "100", now)
			test.mutate(&view)
			reading, err := tracker.Observe(view, 1, now)
			if err != nil || reading.Quality != test.quality || !reading.Mid.IsZero() || !reading.Bid.IsZero() || !reading.Ask.IsZero() {
				t.Fatalf("bad market data retained prices: %+v, %v", reading, err)
			}
		})
	}
}

func TestOlderOrForeignReadingsCannotReplaceSelectedBook(t *testing.T) {
	tracker := fixture(t, domain.MarketSourceBinance)
	now := time.Unix(1800000000, 0)
	warm(t, tracker, domain.MarketSourceBinance, now)
	foreign := sample(domain.MarketSourceOKX, 4, "200", now.Add(time.Second))
	if _, err := tracker.Observe(foreign, 2, now.Add(time.Second)); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("foreign source accepted")
	}
	if _, err := tracker.Observe(sample(domain.MarketSourceBinance, 2, "200", now.Add(time.Second)), 1, now.Add(time.Second)); !errors.Is(err, domain.ErrOldMarketReading) {
		t.Fatal("older sequence accepted")
	}
	if current := tracker.Current(now.Add(time.Second)); current.Mid.String() != "100" || current.Quality != domain.MarketQualityHealthy {
		t.Fatal("unrelated/old message poisoned healthy source")
	}
	clock := tracker.Current(now)
	if clock.Quality != domain.MarketQualityClock || !clock.Mid.IsZero() {
		t.Fatal("backwards wall clock preserved authorization")
	}
}

func TestSequenceIdentityCannotCarryDifferentPrices(t *testing.T) {
	tracker := fixture(t, domain.MarketSourceBinance)
	now := time.Unix(1800000000, 0)
	warm(t, tracker, domain.MarketSourceBinance, now)
	view := sample(domain.MarketSourceBinance, 3, "101", now.Add(200*time.Millisecond))
	if reading, err := tracker.Observe(view, 1, now.Add(200*time.Millisecond)); !errors.Is(err, domain.ErrUnreliableMarket) || !reading.Mid.IsZero() {
		t.Fatal("same sequence with different values reused a reference")
	}
}

func TestNewConnectionOrMaintenanceGenerationMustWarmAgain(t *testing.T) {
	for _, reconnect := range []bool{true, false} {
		tracker := fixture(t, domain.MarketSourceOKX)
		now := time.Unix(1800000000, 0)
		warm(t, tracker, domain.MarketSourceOKX, now)
		view := sample(domain.MarketSourceOKX, 1, "100", now.Add(time.Second))
		connection := int64(1)
		if reconnect {
			connection = 2
		} else {
			view.Generation = 2
		}
		reading, err := tracker.Observe(view, connection, now.Add(time.Second))
		if err != nil || reading.Quality != domain.MarketQualityWarming || !reading.Mid.IsZero() {
			t.Fatalf("new connection/book skipped recovery: %+v, %v", reading, err)
		}
	}
}

func TestQualityPolicyRejectsUnsafeThresholds(t *testing.T) {
	for _, change := range []func(*Policy){
		func(policy *Policy) { policy.MaximumAge = 6 * time.Second },
		func(policy *Policy) { policy.MaximumSkew = 2 * time.Second },
		func(policy *Policy) { policy.RecoverySamples = 1 },
		func(policy *Policy) { policy.RecoveryWindow = policy.MaximumAge },
		func(policy *Policy) { policy.MaximumSpreadBP = decimal.Zero },
		func(policy *Policy) { policy.MaximumJumpBP = decimal.New(1, 2000000000) },
	} {
		policy := DefaultPolicy()
		change(&policy)
		if _, err := New(domain.MarketSourceBinance, marketwire.Instrument{
			ID:     1,
			Symbol: "BTCUSDT",
		}, domain.MarketModeLive, policy); err == nil {
			t.Fatal("unbounded or unsafe quality policy accepted")
		}
	}
}

func TestInvalidationRequiresFreshRecoveryAndKeepsFractionalMid(t *testing.T) {
	tracker := fixture(t, domain.MarketSourceBinance)
	now := time.Unix(1800000000, 0)
	warm(t, tracker, domain.MarketSourceBinance, now)
	if reading := tracker.Invalidate(domain.MarketReasonSequence, now.Add(time.Second)); !reading.Mid.IsZero() || reading.Quality != domain.MarketQualityInvalid {
		t.Fatal("gap invalidation retained old price")
	}
	var reading domain.MarketReading
	for index := int64(1); index <= 3; index++ {
		at := now.Add(time.Second + time.Duration(index)*100*time.Millisecond)
		view := sample(domain.MarketSourceBinance, index, "100", at)
		view.Bid.Price = decimal.RequireFromString("99.999999999999999999")
		view.Ask.Price = decimal.NewFromInt(100)
		view.Mid = decimal.RequireFromString("99.9999999999999999995")
		var err error
		reading, err = tracker.Observe(view, 2, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reading.Mid.String() != "99.9999999999999999995" || reading.Selected(domain.MarketSourceBinance, 1, "BTCUSDT", domain.MarketModeReplay, reading.EvaluatedAt, time.Second) != nil {
		t.Fatalf("reference price was rounded: %+v", reading)
	}
}

func fixture(t *testing.T, source domain.MarketSource) *Tracker {
	t.Helper()
	tracker, err := New(source, marketwire.Instrument{
		ID:     1,
		Symbol: "BTCUSDT",
	}, domain.MarketModeReplay, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return tracker
}

func sample(source domain.MarketSource, sequence int64, mid string, now time.Time) marketbook.View {
	price := decimal.RequireFromString(mid)
	return marketbook.View{
		Source:       source,
		InstrumentID: 1,
		Symbol:       "BTCUSDT",
		Generation:   1,
		Sequence:     sequence,
		Bid: marketbook.Level{
			Price:    price.Sub(decimal.RequireFromString("0.01")),
			Quantity: decimal.NewFromInt(1),
		},
		Ask: marketbook.Level{
			Price:    price.Add(decimal.RequireFromString("0.01")),
			Quantity: decimal.NewFromInt(1),
		},
		Mid:        price,
		EventTime:  now.Add(-10 * time.Millisecond),
		ReceivedAt: now,
	}
}

func warm(t *testing.T, tracker *Tracker, source domain.MarketSource, now time.Time) {
	t.Helper()
	for index := int64(1); index <= 3; index++ {
		at := now.Add(time.Duration(index-1) * 100 * time.Millisecond)
		reading, err := tracker.Observe(sample(source, index, "100", at), 1, at)
		if err != nil || index == 3 && reading.Quality != domain.MarketQualityHealthy {
			t.Fatalf("warmup: %+v, %v", reading, err)
		}
	}
}
