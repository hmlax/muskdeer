//go:build integration

package migrate

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminEmergencyRPCProcessConfirmsScopeAndQueriesWithoutAdvancingExecution(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "native-emergency-viewer", domain.AdminRoleViewer, 211, domain.AdminSessionTTL)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	config := businessProcessConfig(t, ca, postgres, nil, discovery.Config, "strategy")
	config.Business.Strategy = &configv1.StrategyRuntime{
		ManagementEnabled: true,
		ControlEnabled:    true,
	}
	pid := startBusinessProcess(ctx, t, config, discovery.Client)
	if pid == os.Getpid() || pid <= 0 {
		t.Fatal("management emergency RPC did not run in an independent process")
	}
	connection := adminStrategyProcessConnection(ctx, t, ca, discovery.Client, rpcsecurity.ServiceAPI)
	writer := strategyv1.NewStrategyEmergencyCancellationControlClient(connection)
	reader := strategyv1.NewStrategyEmergencyCancellationQueryClient(connection)
	command := &strategyv1.RequestEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: actor.Token},
		CsrfToken:      actor.CSRF,
		IdempotencyKey: "native-emergency-rpc-request-0001",
		StrategyId:     before.Id,
		Input: &strategyv1.EmergencyCancellationInput{
			Scope:                string(domain.AdminEmergencyCancelScopeStrategy),
			ExpectedRiskRevision: 1,
			Reason:               "native explicit strategy emergency scope",
		},
	}
	accepted, err := writer.RequestEmergencyCancellation(ctx, command)
	if err != nil || accepted.StrategyId != before.Id || accepted.ExpectedRiskRevision != 1 || accepted.RiskRevision != 2 {
		t.Fatal("native emergency RPC lost independent risk confirmation", err)
	}
	if replay, err := writer.RequestEmergencyCancellation(ctx, command); err != nil || !proto.Equal(accepted, replay) {
		t.Fatal("native emergency replay created a second risk closure", err)
	}
	assertEmergencyRPCMutationRejections(ctx, t, writer, command, viewer)
	other := adminStrategyProcessConnection(ctx, t, ca, discovery.Client, rpcsecurity.ServicePortfolio)
	query := &strategyv1.GetEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: viewer.Token},
		StrategyId:     before.Id,
		CancellationId: accepted.Id,
	}
	if _, err := strategyv1.NewStrategyEmergencyCancellationQueryClient(other).GetEmergencyCancellation(ctx, query); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API certificate read emergency scope", err)
	}
	if _, err := strategyv1.NewStrategyEmergencyCancellationControlClient(other).RequestEmergencyCancellation(ctx, command); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API certificate mutated emergency scope", err)
	}
	assertEmergencyRPCObservation(ctx, t, reader, query, accepted)
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.RequestEmergencyCancellation(ctx, command); status.Code(err) != codes.Unauthenticated {
		t.Fatal("revoked actor replayed emergency mutation", err)
	}
	if err := auth.Logout(ctx, viewer.Token, viewer.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.GetEmergencyCancellation(ctx, query); status.Code(err) != codes.Unauthenticated {
		t.Fatal("revoked viewer retained emergency observation", err)
	}
	current, err := intents.ByID(ctx, submit.Input.AccountID, order.ID)
	if err != nil || current.State != domain.OrderStateLive || !current.ReservedQuoteNotional.Equal(order.ReservedQuoteNotional) ||
		!proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("management transport changed the real order, funds or desired runtime", err)
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellation{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminStrategyRiskChange{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
}

func assertEmergencyRPCObservation(ctx context.Context, t *testing.T, reader strategyv1.StrategyEmergencyCancellationQueryClient, query *strategyv1.GetEmergencyCancellationRequest, accepted *strategyv1.EmergencyCancellationAcceptance) {
	t.Helper()
	observed, err := reader.GetEmergencyCancellation(ctx, query)
	if err != nil || observed.Record == nil || observed.Progress == nil || !proto.Equal(observed.Record.Acceptance, accepted) || observed.Record.State != string(domain.AdminEmergencyCancelAccepted) ||
		observed.Record.EnrollmentComplete || observed.Record.CompletedAtMs != nil || observed.Progress.Targets != 0 {
		t.Fatal("management-only read advanced or fabricated execution proof", err)
	}
	history, err := reader.ListEmergencyCancellations(ctx, &strategyv1.ListEmergencyCancellationsRequest{
		Session:    query.Session,
		StrategyId: query.StrategyId,
		Limit:      1,
	})
	if err != nil || len(history.Items) != 1 || !proto.Equal(history.Items[0], observed.Record) || history.NextAfterId != 0 {
		t.Fatal("native emergency history crossed its accepted scope", err)
	}
}

func TestAdminEmergencyRPCQueryOnlyProcessDoesNotRegisterControl(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	actor, _, submit := adminCreationFixture(ctx, t, db)
	accepted, err := emergencyAcceptanceStore(t, db).Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	config := businessProcessConfig(t, ca, postgres, nil, discovery.Config, "strategy")
	config.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, config, discovery.Client)
	connection := adminStrategyProcessConnection(ctx, t, ca, discovery.Client, rpcsecurity.ServiceAPI)
	if _, err := strategyv1.NewStrategyEmergencyCancellationControlClient(connection).RequestEmergencyCancellation(ctx, &strategyv1.RequestEmergencyCancellationRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only process registered emergency writes", err)
	}
	value, err := strategyv1.NewStrategyEmergencyCancellationQueryClient(connection).GetEmergencyCancellation(ctx, &strategyv1.GetEmergencyCancellationRequest{
		Session:        &strategyv1.AdminSession{Token: actor.Token},
		StrategyId:     accepted.StrategyID,
		CancellationId: accepted.ID,
	})
	if err != nil || value.Record.State != string(domain.AdminEmergencyCancelAccepted) || value.Record.EnrollmentComplete || value.Record.CompletedAtMs != nil {
		t.Fatal("query-only process advanced an empty accepted scope", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyAction{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}

func assertEmergencyRPCMutationRejections(ctx context.Context, t *testing.T, writer strategyv1.StrategyEmergencyCancellationControlClient, original *strategyv1.RequestEmergencyCancellationRequest, viewer domain.AdminLogin) {
	t.Helper()
	request := proto.CloneOf(original)
	request.Session.Token, request.CsrfToken = viewer.Token, viewer.CSRF
	if _, err := writer.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("viewer acquired emergency write authority", err)
	}
	request = proto.CloneOf(original)
	request.CsrfToken = viewer.CSRF
	if _, err := writer.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("emergency write skipped independent CSRF", err)
	}
	request = proto.CloneOf(original)
	request.Input.Reason = "changed same-key scope confirmation"
	if _, err := writer.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.Aborted {
		t.Fatal("same-key emergency changed its original confirmation", err)
	}
	request = proto.CloneOf(original)
	request.IdempotencyKey = "native-emergency-stale-key-0001"
	if _, err := writer.RequestEmergencyCancellation(ctx, request); status.Code(err) != codes.Aborted {
		t.Fatal("new emergency request accepted stale risk confirmation", err)
	}
}
