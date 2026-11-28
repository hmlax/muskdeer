//go:build integration

package venue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func makerAdmissionSetup(ctx context.Context, t *testing.T) *currentStateTest {
	t.Helper()
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
	return setup
}

func fixedBook(book *common.VenueBook) func(context.Context) (*common.VenueBook, error) {
	return func(context.Context) (*common.VenueBook, error) { return book, nil }
}

// Admission reads the account policy once and overlaps it with the instance
// configuration check and the execution-side source read. The source read
// below only returns after both database reads have started, so a serialized
// implementation fails instead of passing slowly.
func TestMakerAdmissionOverlapsReadsAndReadsPolicyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	setup := makerAdmissionSetup(ctx, t)
	runtime := setup.runtime
	reads := countTableReads(t, runtime.db, "count_admission_reads")
	book, plan := retentionAdmissionPlan(setup)
	source := func(ctx context.Context) (*common.VenueBook, error) {
		deadline := time.Now().Add(2 * time.Second)
		for reads["trading_account_policies"].Load() == 0 || reads["strategy_instances"].Load() == 0 {
			if time.Now().After(deadline) || ctx.Err() != nil {
				return nil, errors.New("source read did not overlap the database reads")
			}
			time.Sleep(time.Millisecond)
		}
		return book, nil
	}
	if err := runtime.admitMakerIntent(ctx, setup.profile, plan, source); err != nil {
		t.Fatal(err)
	}
	if reads["trading_account_policies"].Load() != 1 || reads["strategy_instances"].Load() != 1 || runtime.mailbox.pending.Load() == nil {
		t.Fatalf("policy reads=%d config reads=%d pending=%v", reads["trading_account_policies"].Load(), reads["strategy_instances"].Load(), runtime.mailbox.pending.Load() != nil)
	}
}

func TestMakerAdmissionUsesCurrentPolicyLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	setup := makerAdmissionSetup(ctx, t)
	runtime := setup.runtime
	if err := setup.db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("id = ?", setup.budget.Id).Update("maximum_order_notional", "5").Error; err != nil {
		t.Fatal(err)
	}
	book, plan := retentionAdmissionPlan(setup)
	before := runtime.plans[setup.budget.AccountId].Load()
	if err := runtime.admitMakerIntent(ctx, setup.profile, plan, fixedBook(book)); err == nil {
		t.Fatal("target above the current single-order limit was admitted")
	}
	if runtime.plans[setup.budget.AccountId].Load() != before || runtime.mailbox.pending.Load() != nil {
		t.Fatal("refused plan renewed retention or reached the writer")
	}
}

func TestMakerAdmissionRefusesWhenSourceReadFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	setup := makerAdmissionSetup(ctx, t)
	runtime := setup.runtime
	_, plan := retentionAdmissionPlan(setup)
	before := runtime.plans[setup.budget.AccountId].Load()
	failed := func(context.Context) (*common.VenueBook, error) { return nil, errors.New("market unavailable") }
	if err := runtime.admitMakerIntent(ctx, setup.profile, plan, failed); err == nil {
		t.Fatal("plan admitted without an execution-side source read")
	}
	if runtime.plans[setup.budget.AccountId].Load() != before || runtime.mailbox.pending.Load() != nil || setup.client.submissions.Load() != 0 {
		t.Fatal("failed source read renewed retention, reached the writer or placed an order")
	}
}
