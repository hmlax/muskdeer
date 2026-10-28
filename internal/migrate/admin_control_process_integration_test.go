//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestAdminControlProcessesRevalidateAuthorityAndProjectOneDurableResult(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	_, auth, viewer, instances := adminStrategiesFixture(ctx, t, db)
	actor, _ := adminControlFixture(ctx, t, db)
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, suffix := range []string{"first", "second"} {
		cfg := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
		cfg.InstanceId += "." + suffix
		cfg.Business.Strategy = &configv1.StrategyRuntime{
			ManagementEnabled: true,
			ControlEnabled:    true,
		}
		startBusinessProcess(ctx, t, cfg, discovery)
	}
	client := strategyv1.NewStrategyControlClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI))
	command := &strategyv1.ControlStrategyRequest{
		Session:          &strategyv1.AdminSession{Token: actor.Token},
		CsrfToken:        actor.CSRF,
		IdempotencyKey:   "native-management-start-0001",
		StrategyId:       instances[0].Id,
		ExpectedRevision: 1,
		Action:           string(domain.AdminControlStart),
		Reason:           "native operator start intent",
	}
	accepted, err := client.ControlStrategy(ctx, command)
	if err != nil || accepted.State != string(domain.AdminOperationAccepted) || accepted.CompletedAtMs != nil || accepted.Observation != nil || accepted.EffectRevision != 2 {
		t.Fatal("native control acceptance invented runtime completion", err)
	}
	replayed, err := client.ControlStrategy(ctx, command)
	if err != nil || replayed.Id != accepted.Id {
		t.Fatal("native replay created a second effect", err)
	}
	assertNativeAdminControlRejections(ctx, t, ca, discovery, client, command, viewer)
	command.IdempotencyKey = "native-management-stop-0001"
	command.ExpectedRevision = 2
	command.Action = string(domain.AdminControlStop)
	command.Reason = "replace an unowned metadata start"
	stop, err := client.ControlStrategy(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	old := waitNativeAdminOperation(ctx, t, client, viewer.Token, accepted.Id, domain.AdminOperationSuperseded)
	if old.Observation.GetState() != string(domain.StrategyStateCreated) || old.Observation.StrategyRevision != 3 {
		t.Fatal("owned result worker fabricated the superseded runtime witness")
	}
	// This unfunded metadata fixture has no runtime owner or orders. Its legacy
	// epoch-zero state is explicit; only the actual service workers write results.
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Where("id = ?", command.StrategyId).
		Select("State", "Revision").Updates(&modelv1.StrategyInstance{
		State:    string(domain.StrategyStateStopped),
		Revision: 4,
	}).Error; err != nil {
		t.Fatal(err)
	}
	completed := waitNativeAdminOperation(ctx, t, client, viewer.Token, stop.Id, domain.AdminOperationSucceeded)
	if completed.CompletedAtMs == nil || completed.Observation.GetState() != string(domain.StrategyStateStopped) || completed.Observation.StrategyRevision != 4 || completed.Revision != 2 {
		t.Fatal("two native observers lost or duplicated the durable completion witness")
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ControlStrategy(ctx, command); status.Code(err) != codes.Unauthenticated {
		t.Fatal("revoked session replayed native control", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyOperation{}, 2)
	for _, entity := range []any{&modelv1.SimulatorAccount{}, &modelv1.StrategyLease{}, &modelv1.StrategyAction{}} {
		assertPortfolioRowCount(ctx, t, db, entity, 0)
	}
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertNativeAdminControlRejections(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, client strategyv1.StrategyControlClient, original *strategyv1.ControlStrategyRequest, viewer domain.AdminLogin) {
	t.Helper()
	otherPeer := strategyv1.NewStrategyControlClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio))
	if _, err := otherPeer.ControlStrategy(ctx, original); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate reached management mutation", err)
	}
	request := proto.CloneOf(original)
	request.Session.Token, request.CsrfToken = viewer.Token, viewer.CSRF
	if _, err := client.ControlStrategy(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("persistent viewer acquired operator control", err)
	}
	request = proto.CloneOf(original)
	request.CsrfToken = viewer.CSRF
	if _, err := client.ControlStrategy(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("independent CSRF mismatch reached effect", err)
	}
	request = proto.CloneOf(original)
	request.Reason = "changed content on original key"
	if _, err := client.ControlStrategy(ctx, request); status.Code(err) != codes.Aborted {
		t.Fatal("native key content mismatch was accepted", err)
	}
	request = proto.CloneOf(original)
	request.IdempotencyKey = "native-management-stale-0001"
	if _, err := client.ControlStrategy(ctx, request); status.Code(err) != codes.Aborted {
		t.Fatal("native stale revision changed desired intent", err)
	}
}

func waitNativeAdminOperation(ctx context.Context, t *testing.T, client strategyv1.StrategyControlClient, token string, id int64, state domain.AdminOperationState) *strategyv1.StrategyOperation {
	t.Helper()
	query, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		operation, err := client.GetOperation(query, &strategyv1.GetOperationRequest{
			Session:     &strategyv1.AdminSession{Token: token},
			OperationId: id,
		})
		if err != nil {
			t.Fatal("native operation query failed", err)
		}
		if operation.State == string(state) {
			return operation
		}
		select {
		case <-ticker.C:
		case <-query.Done():
			t.Fatal("native owned observers did not project the expected result")
		}
	}
}
