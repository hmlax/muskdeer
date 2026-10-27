//go:build integration

package migrate

import (
	"context"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestExecutionProcessRefreshesRiskAndClosesOnSelectedMarketLoss(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	portfolioFixture(ctx, t, db, submit)
	feed, marketOwner, _, _ := admissionSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	expireLeaseFixture(ctx, t, db, submit.ExecutionAuthority.Key)
	expireLeaseFixture(ctx, t, db, marketOwner.Key)
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).
		Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).
		UpdateColumn("allow_new_risk", false).Error; err != nil {
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
	marketCfg := marketProcessConfig(t, ca, postgres, naming, feed.ID, "risk-source", "99.99", "100.01")
	marketPID := startBusinessProcess(ctx, t, marketCfg, observer)
	executionCfg := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	// This is a successful multi-query process scenario, not a 200 ms latency
	// target. The separate forced-lock case verifies the short native deadline.
	executionCfg.Business.OperationTimeout = durationpb.New(time.Second)
	executionCfg.Business.ExecutionLeaseTtl = durationpb.New(3 * time.Second)
	executionCfg.Business.Risk = &configv1.RiskRuntime{AccountIds: []string{submit.Input.AccountID}}
	executionPID := startBusinessProcess(ctx, t, executionCfg, observer)
	if marketPID == executionPID || executionPID == os.Getpid() || marketPID == os.Getpid() {
		t.Fatal("risk and market did not run in independent processes")
	}
	ready := waitProcessRisk(ctx, t, db, submit.Input.Authority.StrategyID, domain.RiskReasonReady)
	if !ready.AllowNewRisk || !ready.Reconciled || ready.GetMarketFeedId() != feed.ID ||
		ready.MarketEpoch <= marketOwner.Epoch || ready.MarketMaximumAgeMs != 500 ||
		ready.ExpiresAt-ready.ObservedAt > 500 || ready.ExpiresAt <= ready.ObservedAt {
		t.Fatalf("risk process omitted selected-market evidence: %+v", ready)
	}
	var lease modelv1.ExecutionShardLease
	if err := db.WithContext(ctx).Model(&modelv1.ExecutionShardLease{}).
		Where("account_id = ?", submit.Input.AccountID).First(&lease).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lease.OwnerId, executionCfg.InstanceId+".") || lease.Epoch <= submit.ExecutionAuthority.Epoch {
		t.Fatal("risk process did not own the account execution lease")
	}
	process, err := os.FindProcess(marketPID)
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	closed := waitProcessRisk(ctx, t, db, submit.Input.Authority.StrategyID, domain.RiskReasonMarket)
	if closed.AllowNewRisk || closed.Revision <= ready.Revision || closed.GetMarketFeedId() != 0 {
		t.Fatal("selected source loss did not close the durable risk grant")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorAccount{}, 1)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func waitProcessRisk(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64, reason domain.RiskReason) *modelv1.RiskSnapshot {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var row modelv1.RiskSnapshot
		if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).
			Where("strategy_instance_id = ?", strategyID).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.ReasonCode == string(reason) {
			return &row
		}
		select {
		case <-ctx.Done():
			t.Fatalf("risk process did not reach %s; last reason=%s", reason, row.ReasonCode)
		case <-ticker.C:
		}
	}
}
