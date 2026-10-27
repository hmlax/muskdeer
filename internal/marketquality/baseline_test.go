package marketquality

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestRestoredBaselineRejectsRestartOutlierAndStillRequiresRecovery(t *testing.T) {
	now := time.Unix(1800000000, 0)
	before := fixture(t, domain.MarketSourceBinance)
	warm(t, before, domain.MarketSourceBinance, now)
	checkpoint := before.Baseline()
	if checkpoint.Mid.String() != "100" || checkpoint.AcceptedAt.IsZero() {
		t.Fatal("accepted baseline was not captured")
	}
	after := fixture(t, domain.MarketSourceBinance)
	at := now.Add(time.Minute)
	if err := after.Restore(checkpoint, at); err != nil {
		t.Fatal(err)
	}
	if current := after.Current(at); current.Quality == domain.MarketQualityHealthy || !current.Mid.IsZero() {
		t.Fatal("stale baseline restored executable prices")
	}
	reading, err := after.Observe(sample(domain.MarketSourceBinance, 1, "200", at), 1, at)
	if err != nil || reading.Quality != domain.MarketQualityOutlier || !reading.Mid.IsZero() {
		t.Fatal("restarting process reset its accepted baseline")
	}
	for sequence := int64(2); sequence <= 4; sequence++ {
		at = now.Add(time.Minute + time.Duration(sequence)*100*time.Millisecond)
		reading, err = after.Observe(sample(domain.MarketSourceBinance, sequence, "100", at), 1, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	if reading.Quality != domain.MarketQualityHealthy {
		t.Fatal("valid recovery failed after restoring baseline")
	}
	if err := after.Restore(checkpoint, at); err == nil {
		t.Fatal("live tracker allowed baseline replacement")
	}
}

func TestBaselineRestoreRejectsOtherScopeFutureAndUnboundedValues(t *testing.T) {
	now := time.Unix(1800000000, 0)
	tracker := fixture(t, domain.MarketSourceBinance)
	warm(t, tracker, domain.MarketSourceBinance, now)
	checkpoint := tracker.Baseline()
	for _, mutate := range []func(*domain.MarketBaseline){
		func(value *domain.MarketBaseline) { value.Source = domain.MarketSourceOKX },
		func(value *domain.MarketBaseline) { value.Mode = domain.MarketModeLive },
		func(value *domain.MarketBaseline) { value.SourceVersion++ },
		func(value *domain.MarketBaseline) { value.InstrumentID++ },
		func(value *domain.MarketBaseline) { value.AcceptedAt = now.Add(time.Hour) },
		func(value *domain.MarketBaseline) { value.Mid = decimal.New(1, 2000000000) },
		func(value *domain.MarketBaseline) { value.Mid = decimal.Zero },
	} {
		candidate := checkpoint
		mutate(&candidate)
		fresh := fixture(t, domain.MarketSourceBinance)
		if err := fresh.Restore(candidate, now.Add(time.Second)); err == nil {
			t.Fatal("unsafe baseline replaced source identity or recovery")
		}
	}
}
