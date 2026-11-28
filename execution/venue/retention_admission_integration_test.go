//go:build integration

package venue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestFreshPlanRetainsQuotesWhileFundWriterIsBusy(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	setup := newCurrentStateTest(ctx, t)
	runtime := setup.runtime
	runtime.cfg.Template = domain.StrategyTemplateMarketMaker
	runtime.store.matching = true
	setup.profile.MaximumOrderNotional = "100"
	runtime.closing = &atomic.Bool{}
	runtime.mailbox = newQuoteMailbox()
	if _, _, err := runtime.read(ctx, setup.profile); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	runtime.gates = map[string]chan struct{}{setup.budget.AccountId: gate}
	old := time.Now().Add(-time.Millisecond).UnixMilli()
	runtime.plans[setup.budget.AccountId].Store(old)
	book, plan := retentionAdmissionPlan(setup)
	if err := runtime.admitMakerIntent(ctx, setup.profile, plan, fixedBook(book)); err != nil {
		t.Fatal(err)
	}
	if !runtime.currentQuotePlan(ctx, book, setup.budget.AccountId) || len(gate) != 1 || runtime.mailbox.pending.Load() == nil || setup.client.submissions.Load() != 0 || setup.client.cancellations.Load() != 0 {
		t.Fatal("fresh read-only plan failed to renew retention without taking the writer or sending SDK operations")
	}
}

func TestRetentionCannotBypassConfigurationSourceOrAccountFreshness(t *testing.T) {
	for _, kind := range []string{"stale_account", "expired_book", "wrong_revision", "oversized_target", "disabled_instance", "disabled_budget", "closing"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			setup := newCurrentStateTest(ctx, t)
			runtime := setup.runtime
			runtime.cfg.Template = domain.StrategyTemplateMarketMaker
			runtime.store.matching = true
			setup.profile.MaximumOrderNotional = "100"
			runtime.closing = &atomic.Bool{}
			runtime.mailbox = newQuoteMailbox()
			if kind != "stale_account" {
				if _, _, err := runtime.read(ctx, setup.profile); err != nil {
					t.Fatal(err)
				}
			}
			book, plan := retentionAdmissionPlan(setup)
			switch kind {
			case "expired_book":
				book.ValidUntilMs = time.Now().Add(-time.Second).UnixMilli()
			case "wrong_revision":
				plan.StrategyRevision++
			case "oversized_target":
				plan.Targets[0].Quantity = "100"
			case "disabled_instance":
				if _, err := instanceconfig.Manage(ctx, setup.db, instanceconfig.ManagementWrite{
					Operation:              instanceconfig.ManagementStop,
					Subject:                "retention-test",
					RequestID:              "retention-test-stop",
					ID:                     setup.instance.Id,
					ExpectedRevision:       setup.instance.Revision,
					ExpectedControlVersion: setup.instance.RequestedConfigVersion,
				}, nil); err != nil {
					t.Fatal(err)
				}
			case "disabled_budget":
				if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Update("enabled", false).Error; err != nil {
					t.Fatal(err)
				}
			case "closing":
				runtime.closing.Store(true)
			}
			before := runtime.plans[setup.budget.AccountId].Load()
			err := runtime.admitMakerIntent(ctx, setup.profile, plan, fixedBook(book))
			if kind != "stale_account" && kind != "closing" && err == nil {
				t.Fatal("invalid retention authorization was accepted")
			}
			if runtime.plans[setup.budget.AccountId].Load() != before || setup.client.submissions.Load() != 0 {
				t.Fatal("invalid plan renewed retention or placed an order")
			}
		})
	}
}

func retentionAdmissionPlan(setup *currentStateTest) (*common.VenueBook, *pb.QuotePlan) {
	book := &common.VenueBook{
		Symbol:       setup.profile.Symbol,
		Source:       native.Source,
		Segment:      native.Segment,
		ValidUntilMs: time.Now().Add(native.BookAge).UnixMilli(),
		Publisher:    &common.VenueAuthority{AccountId: native.MarketKey(setup.profile.Symbol)},
		Bids:         []*common.VenueLevel{{Price: "99"}},
		Asks:         []*common.VenueLevel{{Price: "101"}},
	}
	return book, &pb.QuotePlan{
		StrategyInstanceId: setup.instance.Id,
		StrategyRevision:   setup.instance.Revision,
		Book:               book,
		Strategy:           &common.VenueAuthority{AccountId: native.StrategyKey(setup.profile.UserId, setup.profile.FundId)},
		Targets: []*common.VenueTarget{{
			Side:     string(domain.SideBuy),
			Price:    "99",
			Quantity: "0.1",
		}},
	}
}
