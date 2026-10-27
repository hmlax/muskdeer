package quoting

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestDiffKeepsUnchangedAndSmallRepriceTargets(t *testing.T) {
	input := diffFixture(t)
	input.Orders[0].Input.Price = input.Targets[0].Price.Add(decimal.RequireFromString("0.001"))
	plan, err := Diff(input)
	if err != nil || len(plan.Keep) != 2 || len(plan.Cancel) != 0 || len(plan.Observe) != 0 || len(plan.Submit) != 0 {
		t.Fatalf("unchanged quotes churned: %+v, %v", plan, err)
	}
}

func TestDiffWaitsForTerminalCancellationBeforeAnyReplacement(t *testing.T) {
	input := diffFixture(t)
	input.Targets[0].Price = input.Targets[0].Price.Sub(decimal.NewFromInt(1))
	plan, err := Diff(input)
	if err != nil || len(plan.Cancel) != 1 || plan.Cancel[0] != 1 || len(plan.Keep) != 1 || len(plan.Submit) != 0 {
		t.Fatalf("replacement escaped cancellation barrier: %+v, %v", plan, err)
	}
	input.Orders[0].CancelState = domain.CancelStatePending
	plan, err = Diff(input)
	if err != nil || len(plan.Observe) != 1 || len(plan.Cancel) != 0 || len(plan.Submit) != 0 {
		t.Fatalf("pending cancellation replaced or resent: %+v, %v", plan, err)
	}
	input.Orders[0].State = domain.OrderStateCanceled
	input.Orders[0].CancelState = domain.CancelStateConfirmed
	plan, err = Diff(input)
	if err != nil || len(plan.Submit) != 1 || !plan.Submit[0].Price.Equal(input.Targets[0].Price) {
		t.Fatalf("confirmed terminal order did not permit replacement: %+v, %v", plan, err)
	}
}

func TestDiffProtectsWithoutWaitingForRefreshAndRetainsUncertainty(t *testing.T) {
	for _, state := range []domain.OrderState{domain.OrderStateIntent, domain.OrderStateLive, domain.OrderStatePartiallyFilled, domain.OrderStateSubmitting, domain.OrderStateUnknown} {
		input := diffFixture(t)
		input.Protect = true
		input.LastRefresh = input.Now
		input.Orders[0].State = state
		plan, err := Diff(input)
		if err != nil || len(plan.Submit) != 0 || len(plan.Keep) != 0 || len(plan.Cancel)+len(plan.Observe) != 2 {
			t.Fatalf("protection failed for %s: %+v, %v", state, plan, err)
		}
		if (state == domain.OrderStateSubmitting || state == domain.OrderStateUnknown) && (len(plan.Observe) != 1 || plan.Observe[0] != 1) {
			t.Fatal("uncertain submission was treated as absent or blindly canceled")
		}
		if state == domain.OrderStateIntent && (len(plan.Cancel) != 2 || len(plan.Observe) != 0) {
			t.Fatal("protection did not request atomic cancellation of an unsent intent")
		}
	}
}

func TestDiffAgeUsesImmutableCreationTimeAndBypassesRefreshThrottle(t *testing.T) {
	input := diffFixture(t)
	input.LastRefresh = input.Now
	input.Orders[0].CreatedAt = input.Now.Add(-input.MaxQuoteAge)
	input.Orders[0].UpdatedAt = input.Now
	plan, err := Diff(input)
	if err != nil || len(plan.Cancel) != 1 || plan.Cancel[0] != 1 || len(plan.Submit) != 0 {
		t.Fatalf("recent fill/update refreshed old quote age: %+v, %v", plan, err)
	}
	input = diffFixture(t)
	input.LastRefresh = input.Now
	input.Targets[0].Price = input.Targets[0].Price.Sub(decimal.NewFromInt(1))
	plan, err = Diff(input)
	if err != nil || len(plan.Keep) != 2 || len(plan.Cancel) != 0 || len(plan.Submit) != 0 {
		t.Fatalf("minimum refresh interval ignored: %+v, %v", plan, err)
	}
}

func TestDiffPartialFillAndDuplicateQuotesCannotOverAllocate(t *testing.T) {
	input := diffFixture(t)
	input.Orders[0].State = domain.OrderStatePartiallyFilled
	input.Orders[0].FilledQuantity = input.Orders[0].Input.Quantity
	plan, err := Diff(input)
	if err == nil || len(plan.Submit) != 0 {
		t.Fatal("nonterminal fully filled order accepted")
	}
	input.Orders[0].FilledQuantity = input.Orders[0].Input.Quantity.Div(decimal.NewFromInt(2))
	plan, err = Diff(input)
	if err != nil || len(plan.Cancel) != 1 || len(plan.Submit) != 0 {
		t.Fatal("partial quote replenished before cancellation")
	}
	input = diffFixture(t)
	duplicate := input.Orders[0]
	duplicate.ID = 3
	input.Orders = append(input.Orders, duplicate)
	plan, err = Diff(input)
	if err != nil || len(plan.Keep) != 2 || len(plan.Cancel) != 1 || plan.Cancel[0] != 3 || len(plan.Submit) != 0 {
		t.Fatalf("duplicate quote escaped: %+v, %v", plan, err)
	}
}

func TestDiffRejectsWrongScopeDuplicateIDsInvalidTimesAndExtremeDecimals(t *testing.T) {
	for _, mutate := range []func(*DiffInput){
		func(v *DiffInput) { v.Orders[0].Input.AccountID = "other" },
		func(v *DiffInput) { v.Orders[0].Input.Authority.StrategyID++ },
		func(v *DiffInput) { v.Orders[0].Input.InstrumentID++ },
		func(v *DiffInput) { v.Orders[1].ID = v.Orders[0].ID },
		func(v *DiffInput) { v.Orders[0].CreatedAt = time.Time{} },
		func(v *DiffInput) { v.Orders[0].State = domain.OrderStateUnspecified },
		func(v *DiffInput) { v.Targets[0].Price = decimal.New(1, 2000000000) },
		func(v *DiffInput) { v.RepriceBPS = decimal.New(1, 2000000000) },
		func(v *DiffInput) { v.LastRefresh = v.Now.Add(time.Second) },
	} {
		input := diffFixture(t)
		mutate(&input)
		plan, err := Diff(input)
		if err == nil || len(plan.Submit) != 0 || len(plan.Cancel) != 0 {
			t.Fatalf("invalid diff produced commands: %+v, %v", plan, err)
		}
	}
}

func diffFixture(t *testing.T) DiffInput {
	t.Helper()
	maker := makerFixture()
	targets, err := Maker(maker)
	if err != nil {
		t.Fatal(err)
	}
	orders := make([]domain.IntentSnapshot, 0, len(targets))
	for i, target := range targets {
		orders = append(orders, domain.IntentSnapshot{
			ID: int64(i + 1),
			Input: domain.OrderInput{
				Authority: domain.StrategyAuthority{
					StrategyID: 1,
				},
				AccountID:    "maker",
				InstrumentID: 1,
				Side:         target.Side,
				Price:        target.Price,
				Quantity:     target.Quantity,
				TimeInForce:  target.TimeInForce,
			},
			State:       domain.OrderStateLive,
			CancelState: domain.CancelStateNone,
			CreatedAt:   maker.Now.Add(-200 * time.Millisecond),
			UpdatedAt:   maker.Now.Add(-time.Millisecond),
		})
	}
	return DiffInput{
		StrategyID:         1,
		AccountID:          "maker",
		InstrumentID:       1,
		Targets:            targets,
		Orders:             orders,
		Now:                maker.Now,
		RepriceBPS:         maker.Parameters.RepriceBPS,
		MinRefreshInterval: maker.Parameters.MinRefreshInterval,
		MaxQuoteAge:        maker.Parameters.MaxQuoteAge,
	}
}
