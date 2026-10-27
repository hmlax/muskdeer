package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func rebuildPortfolioRows(ctx context.Context, tx *gorm.DB, row *modelv1.PortfolioAccount, core coreRows) (domain.PortfolioSnapshot, error) {
	stored, err := portfolioSnapshot(row)
	if err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	count, latest, err := portfolioSourceStats(ctx, tx, row.AccountId, row.InstrumentId)
	if err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	var records []*modelv1.PortfolioFill
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Where("account_id = ? AND instrument_id = ? AND delete_at IS NULL", row.AccountId, row.InstrumentId).
		Order("source_sequence ASC").Limit(domain.MaxPortfolioFacts + 1).Find(&records).Error; err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	row.Reconciled = false
	row.ErrorCode = domain.PortfolioErrorIncomplete
	if int64(len(records)) == count && len(records) <= domain.MaxPortfolioFacts && (len(records) == 0 && latest == 0 || len(records) > 0 && records[len(records)-1].SourceSequence == latest) {
		facts := make([]domain.FillFact, 0, len(records))
		for _, record := range records {
			fact, err := portfolioFill(record)
			if err != nil {
				return domain.PortfolioSnapshot{}, err
			}
			facts = append(facts, fact)
		}
		values, err := domain.RebuildPortfolio(stored.Opening, facts)
		if err != nil && !errors.Is(err, domain.ErrProjectionIncomplete) {
			return domain.PortfolioSnapshot{}, err
		}
		row.ErrorCode = domain.PortfolioErrorMismatch
		if err == nil {
			setPortfolioValues(row, values)
			row.LastSourceSequence = latest
			err = comparePortfolioCore(ctx, tx, core, values)
			if err != nil && !errors.Is(err, domain.ErrReconciliationMismatch) {
				return domain.PortfolioSnapshot{}, err
			}
			if err == nil {
				row.Reconciled = true
				row.ErrorCode = ""
			}
		}
	}
	if !row.Reconciled {
		if err := invalidateUnprojectedRisk(ctx, tx, row.AccountId); err != nil {
			return domain.PortfolioSnapshot{}, err
		}
	}
	updated, err := portfolioSnapshot(row)
	if err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	if samePortfolioValues(stored.Values, updated.Values) && stored.LastSourceSequence == updated.LastSourceSequence && stored.Reconciled == updated.Reconciled && stored.ErrorCode == updated.ErrorCode {
		return stored, nil
	}
	previous := row.Revision
	row.Revision++
	result := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("id = ? AND revision = ?", row.Id, previous).
		Select("Quantity", "CashQuote", "CostQuote", "RealizedPnlQuote", "FeesQuote", "LastSourceSequence", "Reconciled", "ErrorCode", "Revision").Updates(row)
	if err := reservationResult(result); err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	return portfolioSnapshot(row)
}

func setPortfolioValues(row *modelv1.PortfolioAccount, values domain.PortfolioValues) {
	row.Quantity = values.Quantity.String()
	row.CashQuote = values.CashQuote.String()
	row.CostQuote = values.CostQuote.String()
	row.RealizedPnlQuote = values.RealizedPnlQuote.String()
	row.FeesQuote = values.FeesQuote.String()
}

func samePortfolioValues(a, b domain.PortfolioValues) bool {
	return a.Quantity.Equal(b.Quantity) && a.CashQuote.Equal(b.CashQuote) && a.CostQuote.Equal(b.CostQuote) && a.RealizedPnlQuote.Equal(b.RealizedPnlQuote) && a.FeesQuote.Equal(b.FeesQuote)
}
