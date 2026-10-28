//go:build integration

package migrate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestStrategyCandidatesUseExactAccountIncludeStopsAndRejectOverflow(t *testing.T) {
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
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	for index := 0; index < domain.MaxAccountStrategyInstances-1; index++ {
		insertCandidateMetadata(ctx, t, db, submit.Input.InstrumentID, fmt.Sprintf("candidate-overflow-%d", index), submit.Input.AccountID)
	}
	if _, err := reader.ForAccount(ctx, submit.Input.AccountID); !errors.Is(err, domain.ErrRiskDenied) {
		t.Fatalf("overflow was silently truncated into an incomplete cycle: %v", err)
	}
}

func insertCandidateMetadata(ctx context.Context, t *testing.T, db *gorm.DB, instrumentID int64, key, account string) int64 {
	t.Helper()
	row := identityInstance(instrumentID, key, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance)
	row.AccountId, row.State, row.DesiredRunning = account, string(domain.StrategyStateStopped), false
	if err := db.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Transaction(func(tx *gorm.DB) error {
		if err := tx.WithContext(ctx).Model(&modelv1.StrategyInstance{}).Create(row).Error; err != nil {
			return err
		}
		parameters := storedParameterFixture(domain.StrategyTemplateMarketMaker)
		parameters.InstanceId = row.Id
		return tx.WithContext(ctx).Model(&modelv1.StrategyConfigVersion{}).Create(parameters).Error
	}); err != nil {
		t.Fatal(err)
	}
	return row.Id
}
