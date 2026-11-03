//go:build integration

package migrate

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestAdminEmergencyProgressUpgradeAddsOnlyPendingMarkerToAcceptedScope(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:33]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	source := riskEventLegacySource(ctx, t, db, instrument.Id, false)
	account := &modelv1.SimulatorAccount{
		AccountId:    source.AccountId,
		SubjectId:    "legacy-emergency-upgrade-subject",
		Enabled:      true,
		QuoteFeeRate: "0",
		Revision:     1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Create(&modelv1.AccountBudget{
		AccountId:        source.AccountId,
		Asset:            instrument.QuoteAsset,
		LimitNotional:    "100",
		ReservedNotional: "0",
		Enabled:          true,
		Revision:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Create(&modelv1.StrategyBudget{
		StrategyInstanceId: source.StrategyInstanceId,
		AccountId:          source.AccountId,
		LimitNotional:      "100",
		ReservedNotional:   "0",
		Enabled:            true,
		Revision:           1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	actor := riskActorFixture(ctx, t, db, "legacy-emergency-operator", domain.AdminRoleOperator, 181, domain.AdminSessionTTL)
	accepted := emergencySchemaCandidate(ctx, t, db, actor, source.StrategyInstanceId, 1)
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellation{}).Create(accepted).Error; err != nil {
		t.Fatal(err)
	}
	before := riskEventSourceRow(ctx, t, db, source.StrategyInstanceId)
	budget := activationBudget(ctx, t, db, source.StrategyInstanceId)
	if version, err := Apply(ctx, db); err != nil || version != 37 {
		t.Fatal("legacy accepted scope progress migration failed", err)
	}
	var task modelv1.AdminEmergencyCancellationTask
	if err := db.WithContext(ctx).Model(&modelv1.AdminEmergencyCancellationTask{}).Where("emergency_cancellation_id = ?", accepted.Id).Take(&task).Error; err != nil ||
		task.StrategyInstanceId != source.StrategyInstanceId || task.State != string(domain.AdminEmergencyCancelAccepted) || task.Revision != 1 ||
		task.EnrollmentUpperId != nil || task.EnrollmentAfterId != 0 || task.EnrollmentComplete || task.CompletedAt != nil || task.CreateAt <= 0 || task.DeleteAt != nil {
		t.Fatal("upgrade invented enrollment/completion or lost its source scope", err)
	}
	if !proto.Equal(before, riskEventSourceRow(ctx, t, db, source.StrategyInstanceId)) || !proto.Equal(budget, activationBudget(ctx, t, db, source.StrategyInstanceId)) {
		t.Fatal("pending marker upgrade changed old risk or money")
	}
	if version, err := Apply(ctx, db); err != nil || version != 37 {
		t.Fatal("progress migration replay failed", err)
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminEmergencyCancellationTask{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.AdminOrderCancellation{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
	assertGatewayOrderCount(ctx, t, db, 0)
}
