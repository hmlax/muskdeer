//go:build integration

package migrate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestStrategyDesiredTargetUpgradeRetainsLegacyPauseAndRunRequests(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyDefinitions(ctx, db, definitions[:23]); err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		key     string
		actual  domain.StrategyState
		running bool
	}{
		{
			key:     "legacy-run-request",
			actual:  domain.StrategyStateCreated,
			running: true,
		},
		{
			key:    "legacy-pause-request",
			actual: domain.StrategyStatePaused,
		},
		{
			key:    "legacy-stop-request",
			actual: domain.StrategyStateStopped,
		},
	} {
		instance := identityInstance(instrument.Id, fixture.key, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
		instance.State, instance.DesiredRunning = string(fixture.actual), fixture.running
		if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
			// This actual v23 fixture predates the new generated field.
			if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Omit("DesiredState").Create(instance).Error; err != nil {
				return err
			}
			parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
			parameters.InstanceId = instance.Id
			return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
		}); err != nil {
			t.Fatal(err)
		}
	}
	if version, err := Apply(ctx, db); err != nil || version != 26 {
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) {
			t.Logf("upgrade database failure: code=%s constraint=%s routine=%s", databaseError.Code, databaseError.ConstraintName, databaseError.Routine)
		}
		t.Fatalf("legacy requested target upgrade failed: version=%d error=%v", version, err)
	}
	var rows []*modelv1.StrategyInstance
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Order("id ASC").Find(&rows).Error; err != nil || len(rows) != 3 {
		t.Fatal("upgrade lost legacy instances")
	}
	for i, target := range []domain.StrategyState{domain.StrategyStateRunning, domain.StrategyStatePaused, domain.StrategyStateStopped} {
		if rows[i].DesiredState != string(target) || rows[i].DesiredRunning != (target == domain.StrategyStateRunning) {
			t.Fatal("upgrade changed a legacy run/pause/stop request")
		}
	}
}
