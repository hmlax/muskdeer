package repository

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type portfolioStore struct{ db *gorm.DB }

// NewPortfolio borrows the database and never infers an opening cost basis.
func NewPortfolio(db *gorm.DB) (domain.PortfolioRepository, error) {
	if db == nil {
		return nil, domain.ErrInvalidOrder
	}
	return &portfolioStore{db: db}, nil
}

func (s *portfolioStore) Initialize(ctx context.Context, opening domain.PortfolioOpening) (domain.PortfolioSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	if !coreIdentifier(opening.AccountID, 64) || opening.InstrumentID <= 0 {
		return domain.PortfolioSnapshot{}, domain.ErrInvalidOrder
	}
	initial, err := domain.RebuildPortfolio(opening, nil)
	if err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	var result domain.PortfolioSnapshot
	err = s.db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Transaction(func(tx *gorm.DB) error {
		core, err := lockCoreRows(ctx, tx, opening.AccountID, opening.InstrumentID)
		if err != nil {
			return err
		}
		var existing modelv1.PortfolioAccount
		err = tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", opening.AccountID).Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing).Error
		if err == nil {
			stored, err := portfolioSnapshot(&existing)
			if err != nil {
				return err
			}
			if existing.DeleteAt != nil || !sameOpening(stored.Opening, opening) {
				return domain.ErrIdempotencyConflict
			}
			result, err = rebuildPortfolioRows(ctx, tx, &existing, core)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		count, _, err := portfolioSourceStats(ctx, tx, opening.AccountID, 0)
		if err != nil {
			return err
		}
		if count != 0 {
			return domain.ErrRiskDenied
		}
		if err := comparePortfolioCore(ctx, tx, core, initial); err != nil {
			return err
		}
		row := &modelv1.PortfolioAccount{
			AccountId:        opening.AccountID,
			InstrumentId:     opening.InstrumentID,
			BaseAsset:        core.instrument.BaseAsset,
			QuoteAsset:       core.instrument.QuoteAsset,
			OpeningQuantity:  opening.Quantity.String(),
			OpeningCashQuote: opening.CashQuote.String(),
			OpeningCostQuote: opening.CostQuote.String(),
			Quantity:         initial.Quantity.String(),
			CashQuote:        initial.CashQuote.String(),
			CostQuote:        initial.CostQuote.String(),
			RealizedPnlQuote: "0",
			FeesQuote:        "0",
			Reconciled:       true,
			Revision:         1,
		}
		if err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Create(row).Error; err != nil {
			return err
		}
		result, err = portfolioSnapshot(row)
		return err
	})
	if err != nil {
		return domain.PortfolioSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func (s *portfolioStore) Apply(ctx context.Context, notice domain.FillNotice) (domain.PortfolioSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	if err := validatePortfolioNotice(notice); err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	return s.withPortfolio(ctx, notice.Fact.AccountID, func(tx *gorm.DB, row *modelv1.PortfolioAccount, core coreRows) (domain.PortfolioSnapshot, error) {
		if err := verifyPortfolioSource(ctx, tx, row, notice.Fact); err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		if err := recordPortfolioInbox(ctx, tx, row, notice); err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		if err := recordPortfolioFill(ctx, tx, notice.Fact); err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		return rebuildPortfolioRows(ctx, tx, row, core)
	})
}

func (s *portfolioStore) Rebuild(ctx context.Context, accountID string) (domain.PortfolioSnapshot, error) {
	return s.withPortfolio(ctx, accountID, func(tx *gorm.DB, row *modelv1.PortfolioAccount, core coreRows) (domain.PortfolioSnapshot, error) {
		return rebuildPortfolioRows(ctx, tx, row, core)
	})
}

func (s *portfolioStore) Get(ctx context.Context, accountID string) (domain.PortfolioSnapshot, error) {
	return s.withPortfolio(ctx, accountID, func(tx *gorm.DB, row *modelv1.PortfolioAccount, core coreRows) (domain.PortfolioSnapshot, error) {
		result, err := portfolioSnapshot(row)
		if err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		_, latest, err := portfolioSourceStats(ctx, tx, accountID, row.InstrumentId)
		if err != nil {
			return domain.PortfolioSnapshot{}, err
		}
		if latest != row.LastSourceSequence {
			result.Reconciled = false
			result.ErrorCode = domain.PortfolioErrorIncomplete
		}
		if err := comparePortfolioCore(ctx, tx, core, result.Values); err != nil {
			if !errors.Is(err, domain.ErrReconciliationMismatch) {
				return domain.PortfolioSnapshot{}, err
			}
			result.Reconciled = false
			result.ErrorCode = domain.PortfolioErrorMismatch
			if err := invalidateUnprojectedRisk(ctx, tx, accountID); err != nil {
				return domain.PortfolioSnapshot{}, err
			}
		}
		return result, nil
	})
}

func (s *portfolioStore) withPortfolio(ctx context.Context, accountID string, apply func(*gorm.DB, *modelv1.PortfolioAccount, coreRows) (domain.PortfolioSnapshot, error)) (domain.PortfolioSnapshot, error) {
	if err := coreContext(ctx); err != nil {
		return domain.PortfolioSnapshot{}, err
	}
	if !coreIdentifier(accountID, 64) {
		return domain.PortfolioSnapshot{}, domain.ErrInvalidOrder
	}
	var identity modelv1.PortfolioAccount
	if err := s.db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ? AND delete_at IS NULL", accountID).First(&identity).Error; err != nil {
		return domain.PortfolioSnapshot{}, coreFailure(ctx, err)
	}
	var result domain.PortfolioSnapshot
	err := s.db.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Transaction(func(tx *gorm.DB) error {
		core, err := lockCoreRows(ctx, tx, accountID, identity.InstrumentId)
		if err != nil {
			return err
		}
		var row modelv1.PortfolioAccount
		if err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ? AND delete_at IS NULL", accountID).
			Clauses(clause.Locking{Strength: "UPDATE"}).First(&row).Error; err != nil {
			return err
		}
		result, err = apply(tx, &row, core)
		return err
	})
	if err != nil {
		return domain.PortfolioSnapshot{}, coreFailure(ctx, err)
	}
	return result, nil
}

func validatePortfolioNotice(notice domain.FillNotice) error {
	fact := notice.Fact
	if err := domain.ValidateFillFact(fact); err != nil {
		return err
	}
	if !coreIdentifier(notice.EventID, 128) || !coreIdentifier(fact.TradeID, 128) || !coreIdentifier(fact.AccountID, 64) || !coreIdentifier(fact.FeeAsset, 32) || len(notice.PayloadHash) != 64 {
		return domain.ErrInvalidOrder
	}
	decoded, err := hex.DecodeString(notice.PayloadHash)
	if err != nil || hex.EncodeToString(decoded) != notice.PayloadHash {
		return domain.ErrInvalidOrder
	}
	return nil
}

func sameOpening(a, b domain.PortfolioOpening) bool {
	return a.AccountID == b.AccountID && a.InstrumentID == b.InstrumentID && a.Quantity.Equal(b.Quantity) && a.CashQuote.Equal(b.CashQuote) && a.CostQuote.Equal(b.CostQuote)
}

func comparePortfolioCore(ctx context.Context, tx *gorm.DB, core coreRows, values domain.PortfolioValues) error {
	base, quote, err := lockBalances(ctx, tx, core)
	if err != nil {
		return err
	}
	baseTotal, err := coreBalanceTotal(base)
	if err != nil {
		return err
	}
	quoteTotal, err := coreBalanceTotal(quote)
	if err != nil {
		return err
	}
	if !values.Quantity.Equal(baseTotal) || !values.CashQuote.Equal(quoteTotal) {
		return domain.ErrReconciliationMismatch
	}
	return nil
}

func coreBalanceTotal(row *modelv1.SimulatorBalance) (decimal.Decimal, error) {
	available, err := decimal.NewFromString(row.Available)
	if err != nil {
		return decimal.Zero, err
	}
	frozen, err := decimal.NewFromString(row.Frozen)
	if err != nil {
		return decimal.Zero, err
	}
	return available.Add(frozen), nil
}
