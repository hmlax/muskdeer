package venue

import (
	"sync/atomic"
	"testing"
	"time"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestMatchingQuoteProtectionUsesFreshDataWithoutDatabaseOwnership(t *testing.T) {
	profile := &common.VenueProfilePolicy{
		UserId: 1,
		FundId: 1,
		Symbol: "BTCUSDT",
	}
	runtime := &Runtime{
		cfg:   &venueconfig.RuntimeConfig{InstanceID: 1},
		store: &Store{matching: true},
		plans: map[string]*atomic.Int64{AccountID(1, 1): {}},
	}
	book := &common.VenueBook{
		Publisher:    &common.VenueAuthority{AccountId: native.MarketKey(profile.Symbol)},
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Bids:         []*common.VenueLevel{{Price: "99"}},
		Asks:         []*common.VenueLevel{{Price: "101"}},
	}
	runtime.rememberQuotePlan(profile, &execution.QuotePlan{}, book)
	if !runtime.currentQuotePlan(t.Context(), book, AccountID(1, 1)) {
		t.Fatal("fresh current plan required a database owner")
	}
	book.ValidUntilMs = time.Now().UnixMilli() - 1
	if runtime.currentQuotePlan(t.Context(), book, AccountID(1, 1)) {
		t.Fatal("stale source protected current quotes")
	}
}
