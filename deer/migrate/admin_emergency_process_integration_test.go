//go:build integration

package migrate

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/testcert"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestAdminEmergencyProcessesEnrollCancelAndFinishAcrossOwnerTakeover(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	order, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	// This second instance has never activated a configuration. Its empty
	// accepted scope still needs the same real account worker to prove completion.
	empty, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	store := emergencyAcceptanceStore(t, db)
	accepted, err := store.Request(ctx, actor.Token, actor.CSRF, emergencyAcceptanceRequest(submit.Input.Authority.StrategyID))
	if err != nil {
		t.Fatal(err)
	}
	emptyRequest := emergencyAcceptanceRequest(empty.StrategyID)
	emptyRequest.Key = "native-empty-emergency-request-0001"
	emptyAccepted, err := store.Request(ctx, actor.Token, actor.CSRF, emptyRequest)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Logout(ctx, actor.Token, actor.CSRF); err != nil {
		t.Fatal(err)
	}
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, lifecycleToken(submit.Input.Authority).Key)
	ca, discovery := emergencyProcessDiscovery(ctx, t)
	execution := businessProcessConfig(t, ca, postgres, businessStream(ctx, t), discovery.Config, "execution")
	execution.Business.OperationTimeout = durationpb.New(time.Second)
	execution.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	execution.Business.Risk = &configv1.RiskRuntime{
		AccountIds: []string{submit.Input.AccountID},
	}
	executionPID := startBusinessProcess(ctx, t, execution, discovery.Client)
	first := strategyProcessConfig(t, ca, postgres, discovery.Config, submit.Input.AccountID, "emergency-first")
	firstPID := startBusinessProcess(ctx, t, first, discovery.Client)
	prior := waitEmergencyProcessCancel(ctx, t, db, accepted, order.ID)
	waitEmergencyProcessTask(ctx, t, db, emptyAccepted.ID, domain.AdminEmergencyCancelSucceeded)
	if current := publicationInstance(ctx, t, db, empty.StrategyID); current.ActiveConfigVersion != nil || current.DesiredRunning {
		t.Fatal("empty cancellation activated configuration or changed desired intent")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	second := strategyProcessConfig(t, ca, postgres, discovery.Config, submit.Input.AccountID, "emergency-second")
	secondPID := startBusinessProcess(ctx, t, second, discovery.Client)
	if firstPID == secondPID || firstPID == executionPID || firstPID == os.Getpid() {
		t.Fatal("emergency workers did not run as independent processes")
	}
	process, err := os.FindProcess(firstPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	replaced := waitEmergencyProcessOwner(ctx, t, db, prior)
	if replaced.RuntimeOwnerId == prior.RuntimeOwnerId {
		t.Fatal("replacement reused the departed incarnation")
	}
	assertEmergencyProcessPending(ctx, t, db, accepted.ID)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 1)
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       order.ClientOrderID,
		TradeID:             "native-emergency-cancel-racing-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ConfirmCancel(ctx, submit.Input.AccountID, order.ClientOrderID); err != nil {
		t.Fatal(err)
	}
	waitEmergencyProcessTask(ctx, t, db, accepted.ID, domain.AdminEmergencyCancelSucceeded)
	assertEmergencyProcessFinal(ctx, t, db, submit, intents, order.ID)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "899.9", "0")
}

type emergencyDiscovery struct {
	Config *configv1.Nacos
	Client *nacos.Client
}

func emergencyProcessDiscovery(ctx context.Context, t *testing.T) (*testcert.Authority, emergencyDiscovery) {
	t.Helper()
	config := businessNaming()
	client, err := nacos.New(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return testcert.New(t), emergencyDiscovery{
		Config: config,
		Client: client,
	}
}

func waitEmergencyProcessCancel(ctx context.Context, t *testing.T, db *gorm.DB, accepted domain.AdminEmergencyCancelAcceptance, orderID int64) *modelv1.StrategyInstance {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var order modelv1.OrderIntent
		if err := db.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("id = ?", orderID).Take(&order).Error; err != nil {
			t.Fatal(err)
		}
		var children int64
		if err := db.WithContext(ctx).Model(&modelv1.AdminOrderCancellation{}).Where("emergency_cancellation_id = ?", accepted.ID).Count(&children).Error; err != nil {
			t.Fatal(err)
		}
		if children == 1 && order.CancelState == string(domain.CancelStatePending) {
			assertEmergencyProcessPending(ctx, t, db, accepted.ID)
			return publicationInstance(ctx, t, db, accepted.StrategyID)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			logStrategyRecoveryState(ctx, t, db, accepted.StrategyID)
			t.Fatal("real strategy worker did not enroll and dispatch accepted scope")
		}
	}
}

func waitEmergencyProcessTask(ctx context.Context, t *testing.T, db *gorm.DB, id int64, state domain.AdminEmergencyCancelState) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var task modelv1.AdminEmergencyCancellationTask
		if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", id).Take(&task).Error; err != nil {
			t.Fatal(err)
		}
		if task.State == string(state) && task.EnrollmentComplete && task.CompletedAt != nil {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("real account worker did not persist scoped completion")
		}
	}
}

func waitEmergencyProcessOwner(ctx context.Context, t *testing.T, db *gorm.DB, prior *modelv1.StrategyInstance) *modelv1.StrategyInstance {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		current := publicationInstance(ctx, t, db, prior.Id)
		if current.RuntimeOwnerEpoch > prior.RuntimeOwnerEpoch {
			return current
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("standby did not resume the scoped work after lease expiry")
		}
	}
}

func assertEmergencyProcessPending(ctx context.Context, t *testing.T, db *gorm.DB, id int64) {
	t.Helper()
	var task modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", id).Take(&task).Error; err != nil ||
		task.State != string(domain.AdminEmergencyCancelAccepted) || !task.EnrollmentComplete || task.CompletedAt != nil {
		t.Fatal("unconfirmed core cancel became scoped completion", err)
	}
}

func assertEmergencyProcessFinal(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, intents domain.IntentRepository, orderID int64) {
	t.Helper()
	order, err := intents.ByID(ctx, submit.Input.AccountID, orderID)
	if err != nil || order.State != domain.OrderStateCanceled || !order.FilledQuantity.Equal(decimal.NewFromInt(1)) || !order.ReservedQuoteNotional.IsZero() {
		t.Fatal("native scoped completion lost terminal funds or racing fill", err)
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "0")
	for _, entity := range []any{&modelv1.OrderIntent{}, &modelv1.SimulatorOrder{}, &modelv1.AdminOrderCancellation{}, &modelv1.StrategyAction{}, &modelv1.CancellationRequest{}, &modelv1.SimulatorFill{}} {
		assertPortfolioRowCount(ctx, t, db, entity, 1)
	}
	if activationBudget(ctx, t, db, submit.Input.Authority.StrategyID).Enabled {
		t.Fatal("task completion restored risk without independent administrator authority")
	}
}
