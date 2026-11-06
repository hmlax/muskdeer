//go:build integration

package migrate

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"

	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	strategymodelv1 "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

func TestStrategyCandidatesDiscoverAllInstancesWithoutCrossingAccounts(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	insertCandidateMetadata(ctx, t, db, submit.Input.InstrumentID, "candidate-foreign", "other-account")
	stoppedID := insertCandidateMetadata(ctx, t, db, submit.Input.InstrumentID, "candidate-stopped", submit.Input.AccountID)
	reader, err := repository.NewStrategyCandidates(db)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := reader.ForAccount(ctx, submit.Input.AccountID)
	if err != nil || !slices.Equal(ids, []int64{submit.Input.Authority.StrategyID, stoppedID}) {
		t.Fatalf("candidate discovery crossed account or omitted a stop: %v, %v", ids, err)
	}
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.StrategyLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.OrderIntent{}, 0)
	additional := make([]*strategymodelv1.StrategyInstance, 0, 255)
	for index := range 255 {
		row := identityInstance(submit.Input.InstrumentID, fmt.Sprintf("candidate-many-%d", index), domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
		row.AccountId, row.State, row.DesiredRunning = submit.Input.AccountID, string(domain.StrategyStateStopped), false
		additional = append(additional, row)
	}
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Create(&additional).Error; err != nil {
			return err
		}
		versions := make([]*strategymodelv1.StrategyConfigVersion, 0, len(additional))
		for _, row := range additional {
			parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
			parameters.InstanceId = row.Id
			versions = append(versions, parameters)
		}
		return tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Create(&versions).Error
	}); err != nil {
		t.Fatal(err)
	}
	all, err := reader.ForAccount(ctx, submit.Input.AccountID)
	if err != nil || len(all) != 257 || !slices.Equal(all[:2], ids) {
		t.Fatalf("ForAccount(257 instances) = %d IDs, %v, want all instances in stable order", len(all), err)
	}
	assertPortfolioRowCount(ctx, t, db, &strategymodelv1.StrategyLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &executionmodelv1.OrderIntent{}, 0)

}

func insertCandidateMetadata(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64, key, account string) int64 {
	t.Helper()
	row := identityInstance(instrumentID, key, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	row.AccountId, row.State, row.DesiredRunning = account, string(domain.StrategyStateStopped), false
	if err := db.WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyInstance{}).Create(row).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = row.Id
		return tx.Session(&gorm.Session{NewDB: true}).WithContext(ctx).Model(&strategymodelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	return row.Id
}
