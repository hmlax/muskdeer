//go:build integration

package migrate

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer/nacos"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestExecutionRoutingUsesCurrentDatabaseLeaseWithoutCreatingRows(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	routing, err := repository.NewExecutionRouting(db)
	if err != nil {
		t.Fatal(err)
	}
	ownerA, err := ownerid.New(ctx, "execution.node.a")
	if err != nil {
		t.Fatal(err)
	}
	ownerB, err := ownerid.New(ctx, "execution.node.b")
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:      domain.LeaseKindExecution,
		AccountID: "route-account-a",
	}
	first, err := leases.Acquire(ctx, key, ownerA, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	other := key
	other.AccountID = "route-account-b"
	if _, err := leases.Acquire(ctx, other, ownerB, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if instance, err := routing.ExecutionInstance(ctx, key.AccountID); err != nil || instance != "execution.node.a" {
		t.Fatalf("account route lost dotted owner identity: %s, %v", instance, err)
	}
	if instance, err := routing.ExecutionInstance(ctx, other.AccountID); err != nil || instance != "execution.node.b" {
		t.Fatal("routing crossed account ownership")
	}
	if instance, err := routing.ExecutionInstance(ctx, "missing"); !errors.Is(err, domain.ErrLeaseLost) || instance != "" {
		t.Fatal("missing owner obtained a fallback route")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.ExecutionShardLease{}, 2)
	expireLeaseFixture(ctx, t, db, key)
	if instance, err := routing.ExecutionInstance(ctx, key.AccountID); !errors.Is(err, domain.ErrLeaseLost) || instance != "" {
		t.Fatal("expired owner remained routable")
	}
	second, err := leases.Acquire(ctx, key, ownerB, 3*time.Second)
	if err != nil || second.Epoch <= first.Epoch {
		t.Fatalf("routing takeover failed: %+v, %v", second, err)
	}
	if instance, err := routing.ExecutionInstance(ctx, key.AccountID); err != nil || instance != "execution.node.b" {
		t.Fatal("routing reused its previous owner after takeover")
	}
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ?", key.AccountID).UpdateColumn("owner_id", "bad.no_nonce").Error; err != nil {
		t.Fatal(err)
	}
	if instance, err := routing.ExecutionInstance(ctx, key.AccountID); !errors.Is(err, domain.ErrLeaseLost) || instance != "" {
		t.Fatal("malformed incarnation obtained a route")
	}
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ?", other.AccountID).UpdateColumn("delete_at", time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if instance, err := routing.ExecutionInstance(ctx, other.AccountID); !errors.Is(err, domain.ErrLeaseLost) || instance != "" {
		t.Fatal("retired lease remained routable")
	}
}

func TestExecutionRoutingFollowsOwnerAcrossIndependentProcesses(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	core, submit := coreFixture(ctx, t, db)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	routing, err := repository.NewExecutionRouting(db)
	if err != nil {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	stream := businessStream(ctx, t)
	first := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	second := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	first.InstanceId, second.InstanceId = "business-execution.a", "business-execution.b"
	for _, cfg := range []*configv1.ServiceConfig{first, second} {
		cfg.Business.OperationTimeout = durationpb.New(time.Second)
		cfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
		cfg.Business.Simulator = &configv1.SimulatorScenarioRuntime{
			AccountIds:  []string{submit.Input.AccountID},
			CancelDelay: durationpb.New(time.Second),
		}
	}
	firstPID := startBusinessProcess(ctx, t, first, observer)
	waitExecutionRoute(ctx, t, routing, submit.Input.AccountID, first.InstanceId)
	var prior modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ?", submit.Input.AccountID).First(&prior).Error; err != nil {
		t.Fatal(err)
	}
	secondPID := startBusinessProcess(ctx, t, second, observer)
	if firstPID == secondPID || firstPID == os.Getpid() || secondPID == os.Getpid() {
		t.Fatal("execution owners were not independent processes")
	}
	if instance, err := routing.ExecutionInstance(ctx, submit.Input.AccountID); err != nil || instance != first.InstanceId {
		t.Fatal("healthy standby was mistaken for the account owner")
	}
	process, err := os.FindProcess(firstPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitExecutionRoute(ctx, t, routing, submit.Input.AccountID, second.InstanceId)
	var current modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ?", submit.Input.AccountID).First(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Epoch <= prior.Epoch || current.OwnerId == prior.OwnerId {
		t.Fatal("process takeover did not advance durable ownership")
	}
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AccountBudget{}, 0)
}

func waitExecutionRoute(ctx context.Context, t *testing.T, routing domain.ExecutionRouting, account, instance string) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := routing.ExecutionInstance(ctx, account)
		if err == nil && current == instance {
			return
		}
		if err != nil && !errors.Is(err, domain.ErrLeaseLost) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("execution route did not observe the expected owner")
		case <-ticker.C:
		}
	}
}
