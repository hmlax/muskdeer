package business

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestSimulatorRuntimeRequiresExplicitAccountsDelayAndLease(t *testing.T) {
	cfg := &configv1.BusinessRuntime{
		ExecutionLeaseTtl: durationpb.New(3 * time.Second),
		Simulator: &configv1.SimulatorScenarioRuntime{
			AccountIds:  []string{"sim-one"},
			CancelDelay: durationpb.New(100 * time.Millisecond),
		},
	}
	if err := validateSimulatorConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.BusinessRuntime){
		func(v *configv1.BusinessRuntime) { v.ExecutionLeaseTtl = nil },
		func(v *configv1.BusinessRuntime) { v.Simulator.AccountIds = nil },
		func(v *configv1.BusinessRuntime) { v.Simulator.AccountIds = []string{"one", "one"} },
		func(v *configv1.BusinessRuntime) { v.Simulator.AccountIds = []string{"bad/account"} },
		func(v *configv1.BusinessRuntime) { v.Simulator.CancelDelay = nil },
		func(v *configv1.BusinessRuntime) { v.Simulator.CancelDelay = durationpb.New(time.Nanosecond) },
		func(v *configv1.BusinessRuntime) { v.Simulator.CancelDelay = durationpb.New(6 * time.Second) },
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if err := validateSimulatorConfig(changed); err == nil {
			t.Fatal("ambiguous simulator scenario scope accepted")
		}
	}
}

func TestSimulatorTickOnlyConfirmsOwnedDueCancellationsAndPreservesFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner := &riskOwnerProbe{failure: domain.ErrLeaseHeld}
	candidates := &simulatorCandidateProbe{client: "client-one"}
	core := &simulatorConfirmProbe{}
	accounts := []string{"sim-one"}
	if worked, err := simulatorCancellationTick(ctx, accounts, time.Millisecond, owner, candidates, core); err != nil || worked || candidates.calls != 0 || core.calls != 0 {
		t.Fatal("standby simulator changed venue state")
	}
	owner.failure = nil
	if worked, err := simulatorCancellationTick(ctx, accounts, time.Millisecond, owner, candidates, core); err != nil || !worked || core.calls != 1 || core.account != accounts[0] || core.client != candidates.client {
		t.Fatal("simulator did not confirm the exact account/client")
	}
	candidates.client = ""
	if worked, err := simulatorCancellationTick(ctx, accounts, time.Millisecond, owner, candidates, core); err != nil || worked || core.calls != 1 {
		t.Fatal("empty simulator scan caused an effect or busy loop")
	}
	candidates.client = "client-one"
	core.failure = errors.New("venue confirmation failure")
	if _, err := simulatorCancellationTick(ctx, accounts, time.Millisecond, owner, candidates, core); !errors.Is(err, core.failure) {
		t.Fatal("simulator hid its original failure")
	}
}

type simulatorCandidateProbe struct {
	client string
	calls  int
}

func (s *simulatorCandidateProbe) DueCancellation(context.Context, string, time.Duration) (string, error) {
	s.calls++
	return s.client, nil
}

type simulatorConfirmProbe struct {
	account string
	client  string
	calls   int
	failure error
}

func (s *simulatorConfirmProbe) ConfirmCancel(_ context.Context, account, client string) (domain.CoreOrder, error) {
	s.account, s.client = account, client
	s.calls++
	return domain.CoreOrder{}, s.failure
}
