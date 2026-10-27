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

func TestRiskRuntimeRequiresExplicitBoundedAccountScopeAndLease(t *testing.T) {
	cfg := &configv1.ServiceConfig{
		Name:            "execution",
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Grpc:            &configv1.Listener{Address: "127.0.0.1:0"},
		GrpcSecurity:    &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}},
		Postgres:        &configv1.Postgres{},
		Jetstream:       &configv1.JetStream{},
		Nacos:           &configv1.Nacos{Registration: &configv1.NacosRegistration{}},
		Business: &configv1.BusinessRuntime{
			Enabled:           true,
			ExecutionVenue:    domain.ExecutionVenueSimulator,
			PollInterval:      durationpb.New(20 * time.Millisecond),
			OperationTimeout:  durationpb.New(200 * time.Millisecond),
			ExecutionLeaseTtl: durationpb.New(3 * time.Second),
			Risk:              &configv1.RiskRuntime{AccountIds: []string{"account-one"}},
		},
	}
	if err := ValidateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(v *configv1.ServiceConfig) { v.Business.Risk.AccountIds = nil },
		func(v *configv1.ServiceConfig) { v.Business.Risk.AccountIds = []string{"same", "same"} },
		func(v *configv1.ServiceConfig) { v.Business.Risk.AccountIds = []string{"bad/account"} },
		func(v *configv1.ServiceConfig) { v.Business.ExecutionLeaseTtl = nil },
		func(v *configv1.ServiceConfig) { v.Business.ExecutionLeaseTtl = durationpb.New(400 * time.Millisecond) },
		func(v *configv1.ServiceConfig) {
			v.Name = "portfolio"
			v.GrpcClients = &configv1.GRPCClients{}
			v.Business.PortfolioConsumer = "portfolio"
		},
	} {
		changed := proto.CloneOf(cfg)
		mutate(changed)
		if err := ValidateConfig(changed); err == nil {
			t.Fatal("unbounded/ambiguous risk scope accepted")
		}
	}
}

func TestRiskTickDoesNotEvaluateStandbyAndPreservesFailures(t *testing.T) {
	owner := &riskOwnerProbe{failure: domain.ErrLeaseHeld}
	candidates := &riskCandidateProbe{ids: []int64{1, 2}}
	evaluator := &riskEvaluatorProbe{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if worked, err := riskEvaluationTick(ctx, "account", owner, candidates, evaluator); err != nil || worked || candidates.calls != 0 || evaluator.calls != 0 {
		t.Fatal("standby evaluated or opened risk")
	}
	owner.failure = nil
	if worked, err := riskEvaluationTick(ctx, "account", owner, candidates, evaluator); err != nil || worked || evaluator.calls != 2 {
		t.Fatal("owned risk tick did not evaluate exactly its candidates")
	}
	evaluator.failure = errors.New("owned evaluator failure")
	if _, err := riskEvaluationTick(ctx, "account", owner, candidates, evaluator); !errors.Is(err, evaluator.failure) {
		t.Fatal("risk task hid its original failure")
	}
}

type riskOwnerProbe struct{ failure error }

func (r *riskOwnerProbe) ForAccount(_ context.Context, account string) (domain.LeaseToken, error) {
	return domain.LeaseToken{
		Key: domain.LeaseKey{
			Kind:      domain.LeaseKindExecution,
			AccountID: account,
		},
		OwnerID: "node.01234567890123456789012345678901",
		Epoch:   1,
	}, r.failure
}

type riskCandidateProbe struct {
	ids   []int64
	calls int
}

func (r *riskCandidateProbe) DesiredForAccount(context.Context, string) ([]int64, error) {
	r.calls++
	return r.ids, nil
}

type riskEvaluatorProbe struct {
	calls   int
	failure error
}

func (r *riskEvaluatorProbe) Evaluate(context.Context, int64) (domain.RiskDecision, error) {
	r.calls++
	return domain.RiskDecision{}, r.failure
}
