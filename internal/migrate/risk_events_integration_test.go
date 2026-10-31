//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestRiskSnapshotEventsRecordMaterialProducerChangesWithoutHeartbeatStorm(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	strategyID := submit.Input.Authority.StrategyID
	initial := riskEventRows(ctx, t, db, strategyID)
	if len(initial) != 1 || initial[0].Kind != string(domain.RiskSnapshotEventBaseline) || initial[0].RiskSnapshotRevision != 1 || initial[0].SnapshotDeleted {
		t.Fatal("recording start was absent or invented a historical transition")
	}
	portfolioFixture(ctx, t, db, submit)
	evaluator, err := repository.NewRiskEvaluator(db)
	if err != nil {
		t.Fatal(err)
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if decision, err := evaluator.Evaluate(ctx, strategyID); err != nil || !decision.Allowed || decision.Reason != domain.RiskReasonReady {
		t.Fatal("actual producer failed to reach ready", err)
	}
	ready := riskEventRows(ctx, t, db, strategyID)
	if len(ready) != 2 || ready[1].Kind != string(domain.RiskSnapshotEventChanged) || ready[1].ReasonCode != string(domain.RiskReasonReady) || !ready[1].Reconciled || !ready[1].AllowNewRisk {
		t.Fatal("material eligibility diagnostic change was lost")
	}
	refreshRiskSelectedMarket(ctx, t, db, submit.Input.InstrumentID)
	if decision, err := evaluator.Evaluate(ctx, strategyID); err != nil || !decision.Allowed {
		t.Fatal(err)
	}
	current := riskEventSourceRow(ctx, t, db, strategyID)
	if current.MarketSequence <= ready[1].MarketSequence || current.Revision <= ready[1].RiskSnapshotRevision || len(riskEventRows(ctx, t, db, strategyID)) != 2 {
		t.Fatal("normal sequence/revision/expiry refresh became a risk event storm")
	}
	core, intents := executionFixture(t, db)
	submit.Input.Deadline = time.Now().Add(time.Second)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	request := riskEventFillRequest(submit, live.ClientOrderID)
	fill, err := core.Fill(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertRiskEventUnreconciled(ctx, t, db, submit, 3)
	if duplicate, err := core.Fill(ctx, request); err != nil || duplicate.ID != fill.ID {
		t.Fatal("duplicate core fill did not retain natural identity", err)
	}
	assertRiskEventUnreconciled(ctx, t, db, submit, 3)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "100.1")
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
}

func TestRiskSnapshotEventAndSourceAndCoreFillRollBackTogether(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, intents := executionFixture(t, db)
	live, err := intentServiceFixture(t, intents, core).Submit(ctx, submit.Input, submit.ExecutionAuthority)
	if err != nil {
		t.Fatal(err)
	}
	before := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	failure := errors.New("controlled interruption after risk event was inserted")
	active := true
	var provisional int64
	const callback = "test:risk-event-after-source-write"
	if err := db.Callback().Update().After("gorm:update").Register(callback, func(query *gorm.DB) {
		if _, snapshot := query.Statement.Dest.(*modelv1.RiskSnapshot); snapshot && active {
			if err := query.WithContext(ctx).Session(&gorm.Session{NewDB: true}).Model(&modelv1.RiskSnapshotEvent{}).
				Where("risk_snapshot_id = ?", before.Id).Count(&provisional).Error; err != nil {
				_ = query.AddError(err)
			}
			_ = query.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Callback().Update().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	request := riskEventFillRequest(submit, live.ClientOrderID)
	if _, err := core.Fill(ctx, request); !errors.Is(err, failure) || provisional != 2 {
		t.Fatal("core fill ignored risk/history transaction failure", err)
	}
	if !proto.Equal(before, riskEventSourceRow(ctx, t, db, before.StrategyInstanceId)) || len(riskEventRows(ctx, t, db, before.StrategyInstanceId)) != 1 {
		t.Fatal("failed core transaction retained a risk source or history effect")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "799.8", "200.2")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "BTC", "10", "0")
	active = false
	if _, err := core.Fill(ctx, request); err != nil {
		t.Fatal("rolled-back trade could not recover with the same identity", err)
	}
	assertRiskEventUnreconciled(ctx, t, db, submit, 2)
}

func TestRiskSnapshotEventsKeepRetirementRemovalAndImmutableHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	rows := riskEventRows(ctx, t, db, submit.Input.Authority.StrategyID)
	if len(rows) != 1 {
		t.Fatal("missing baseline")
	}
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).Where("id = ?", rows[0].Id).UpdateColumn("reason_code", "rewritten-history").Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).Where("id = ?", rows[0].Id).UpdateColumn("delete_at", time.Now().UnixMilli()).Error, "23514")
	assertDatabaseCode(t, db.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).Where("id = ?", rows[0].Id).Delete(&modelv1.RiskSnapshotEvent{}).Error, "23514")
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	retired := time.Now().UnixMilli()
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).
		Select("DeleteAt", "Revision").Updates(&modelv1.RiskSnapshot{
		DeleteAt: &retired,
		Revision: source.Revision + 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	retirement := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(retirement) != 2 || retirement[1].Kind != string(domain.RiskSnapshotEventChanged) || !retirement[1].SnapshotDeleted {
		t.Fatal("retired source did not preserve its historical state")
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("id = ?", source.Id).Delete(&modelv1.RiskSnapshot{}).Error; err != nil {
		t.Fatal(err)
	}
	removed := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(removed) != 3 || removed[2].Kind != string(domain.RiskSnapshotEventRemoved) || !removed[2].SnapshotDeleted || removed[2].RiskSnapshotId != source.Id {
		t.Fatal("source removal erased or fabricated diagnostic history")
	}
}

func riskEventRows(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64) []*modelv1.RiskSnapshotEvent {
	t.Helper()
	var rows []*modelv1.RiskSnapshotEvent
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshotEvent{}).Where("strategy_instance_id = ?", strategyID).Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func riskEventSourceRow(ctx context.Context, t *testing.T, db *gorm.DB, strategyID int64) *modelv1.RiskSnapshot {
	t.Helper()
	var row modelv1.RiskSnapshot
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", strategyID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}

func riskEventFillRequest(submit domain.GatewaySubmit, clientID string) domain.CoreFillRequest {
	return domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       clientID,
		TradeID:             "risk-event-transaction-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}
}

func assertRiskEventUnreconciled(ctx context.Context, t *testing.T, db *gorm.DB, submit domain.GatewaySubmit, count int) {
	t.Helper()
	source := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
	rows := riskEventRows(ctx, t, db, source.StrategyInstanceId)
	if len(rows) != count {
		t.Fatal("source replay lost or duplicated material diagnostic history")
	}
	last := rows[len(rows)-1]
	if last.Kind != string(domain.RiskSnapshotEventChanged) || last.RiskSnapshotId != source.Id || last.RiskSnapshotRevision != source.Revision || last.Reconciled || !last.AllowNewRisk || last.AccountId != submit.Input.AccountID || last.InstrumentId != submit.Input.InstrumentID || last.ReasonCode != source.ReasonCode {
		t.Fatal("history concealed raw flags, transaction identity or account scope")
	}
}
