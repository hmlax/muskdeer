package venue

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

func TestMakerUsesAcceptedNewTargetAfterAccountObservation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"latest", "expired_target", "wrong_revision", "expired_source"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			runtime := &Runtime{
				cfg: &venueconfig.RuntimeConfig{
					InstanceID: 1,
					Revision:   2,
				},
				store:   &Store{matching: true},
				mailbox: newQuoteMailbox(),
			}
			profile := &common.VenueProfilePolicy{
				UserId: 10001,
				FundId: 10001,
				Symbol: "BTCUSDT",
			}
			previous := mailboxTestPlan(10)
			previous.StrategyInstanceId, previous.StrategyRevision = 1, 2
			previous.Strategy = &common.VenueAuthority{AccountId: native.StrategyKey(10001, 10001)}
			previous.Book.Symbol, previous.Book.Source, previous.Book.Segment = profile.Symbol, native.Source, native.Segment
			previous.Book.Publisher = &common.VenueAuthority{AccountId: native.MarketKey(profile.Symbol)}
			previous.Book.ValidUntilMs = time.Now().Add(400 * time.Millisecond).UnixMilli()
			next := proto.CloneOf(previous)
			next.Book.Sequence++
			next.Book.ValidUntilMs = time.Now().Add(native.BookAge).UnixMilli()
			next.Targets[0].Price = "98"
			fresh := proto.CloneOf(next.Book)
			switch kind {
			case "expired_target":
				next.Book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "wrong_revision":
				next.StrategyRevision++
			case "expired_source":
				fresh.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			}
			runtime.mailbox.offer(next)
			got, book, err := runtime.adoptMakerIntent(t.Context(), profile, previous, fresh)
			if kind == "wrong_revision" || kind == "expired_source" {
				if err == nil {
					t.Fatal("invalid replacement source/authority accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantPrice, wantExpiry := "98", next.Book.ValidUntilMs
			if kind == "expired_target" {
				wantPrice, wantExpiry = "99", previous.Book.ValidUntilMs
			}
			if got.Targets[0].Price != wantPrice || book.ValidUntilMs != wantExpiry {
				t.Fatalf("target=%s expiry=%d, want %s/%d", got.Targets[0].Price, book.ValidUntilMs, wantPrice, wantExpiry)
			}
			if previous.Book.ValidUntilMs >= next.Book.ValidUntilMs && kind == "latest" {
				t.Fatal("test did not represent a delayed account observation")
			}
		})
	}
}
