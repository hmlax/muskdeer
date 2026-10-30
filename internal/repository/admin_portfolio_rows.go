package repository

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type adminPortfolioKey struct {
	account    string
	instrument int64
}

type adminPortfolioBalanceKey struct {
	account string
	asset   string
}

type adminPortfolioRelations struct {
	accounts    map[string]*modelv1.SimulatorAccount
	instruments map[int64]*modelv1.Instrument
	balances    map[adminPortfolioBalanceKey]*modelv1.SimulatorBalance
	sources     map[adminPortfolioKey]domain.AdminPortfolioFacts
	collected   map[adminPortfolioKey]domain.AdminPortfolioFacts
}

func readAdminPortfolios(ctx context.Context, tx *gorm.DB, rows []*modelv1.PortfolioAccount) ([]domain.AdminPortfolio, error) {
	result := make([]domain.AdminPortfolio, 0, len(rows))
	if len(rows) == 0 {
		return result, nil
	}
	related, err := readAdminPortfolioRelations(ctx, tx, rows)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		value, err := adminPortfolioValue(row, related)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func readAdminPortfolioRelations(ctx context.Context, tx *gorm.DB, rows []*modelv1.PortfolioAccount) (adminPortfolioRelations, error) {
	accountIDs, instrumentIDs := make([]string, 0, len(rows)), make([]int64, 0, len(rows))
	assets := make([]string, 0, 2*len(rows))
	for _, row := range rows {
		accountIDs = append(accountIDs, row.AccountId)
		instrumentIDs = append(instrumentIDs, row.InstrumentId)
		assets = append(assets, row.BaseAsset, row.QuoteAsset)
	}
	var accounts []*modelv1.SimulatorAccount
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).
		Select("Id", "AccountId", "Enabled", "Revision").Where("account_id IN ? AND delete_at IS NULL", accountIDs).Find(&accounts).Error; err != nil {
		return adminPortfolioRelations{}, err
	}
	var instruments []*modelv1.Instrument
	if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id IN ? AND delete_at IS NULL", instrumentIDs).Find(&instruments).Error; err != nil {
		return adminPortfolioRelations{}, err
	}
	var balances []*modelv1.SimulatorBalance
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).
		Where("account_id IN ? AND asset IN ? AND delete_at IS NULL", accountIDs, assets).Find(&balances).Error; err != nil {
		return adminPortfolioRelations{}, err
	}
	sources, collected, err := readAdminPortfolioStats(ctx, tx, rows)
	if err != nil {
		return adminPortfolioRelations{}, err
	}
	result := adminPortfolioRelations{
		accounts:    make(map[string]*modelv1.SimulatorAccount, len(accounts)),
		instruments: make(map[int64]*modelv1.Instrument, len(instruments)),
		balances:    make(map[adminPortfolioBalanceKey]*modelv1.SimulatorBalance, len(balances)),
		sources:     sources,
		collected:   collected,
	}
	for _, row := range accounts {
		result.accounts[row.AccountId] = row
	}
	for _, row := range instruments {
		result.instruments[row.Id] = row
	}
	for _, row := range balances {
		result.balances[adminPortfolioBalanceKey{
			account: row.AccountId,
			asset:   row.Asset,
		}] = row
	}
	return result, nil
}

func adminPortfolioValue(row *modelv1.PortfolioAccount, related adminPortfolioRelations) (domain.AdminPortfolio, error) {
	account, instrument := related.accounts[row.AccountId], related.instruments[row.InstrumentId]
	if account == nil || instrument == nil || account.Revision <= 0 || row.Id <= 0 || row.CreateAt <= 0 || row.UpdateAt <= 0 ||
		instrument.ExecutionVenue != domain.ExecutionVenueSimulator || instrument.BaseAsset != row.BaseAsset || instrument.QuoteAsset != row.QuoteAsset {
		return domain.AdminPortfolio{}, domain.ErrAdminUnavailable
	}
	snapshot, err := portfolioSnapshot(row)
	if err != nil {
		return domain.AdminPortfolio{}, err
	}
	if !validAdminPortfolioSnapshot(snapshot) {
		return domain.AdminPortfolio{}, domain.ErrAdminUnavailable
	}
	base, err := adminPortfolioBalance(related.balances[adminPortfolioBalanceKey{
		account: row.AccountId,
		asset:   row.BaseAsset,
	}])
	if err != nil {
		return domain.AdminPortfolio{}, err
	}
	quote, err := adminPortfolioBalance(related.balances[adminPortfolioBalanceKey{
		account: row.AccountId,
		asset:   row.QuoteAsset,
	}])
	if err != nil {
		return domain.AdminPortfolio{}, err
	}
	key := adminPortfolioKey{
		account:    row.AccountId,
		instrument: row.InstrumentId,
	}
	return domain.AdminPortfolio{
		ID:               row.Id,
		ExecutionVenue:   instrument.ExecutionVenue,
		Symbol:           instrument.Symbol,
		AccountEnabled:   account.Enabled,
		AccountRevision:  account.Revision,
		Projection:       snapshot,
		Source:           related.sources[key],
		Collected:        related.collected[key],
		CoreBaseBalance:  base,
		CoreQuoteBalance: quote,
		CreatedAt:        time.UnixMilli(row.CreateAt).UTC(),
		UpdatedAt:        time.UnixMilli(row.UpdateAt).UTC(),
	}, nil
}

func validAdminPortfolioSnapshot(value domain.PortfolioSnapshot) bool {
	if value.Revision <= 0 || value.LastSourceSequence < 0 || !coreIdentifier(value.AccountID, 64) || value.InstrumentID <= 0 ||
		!coreIdentifier(value.BaseAsset, 32) || !coreIdentifier(value.QuoteAsset, 32) || value.BaseAsset == value.QuoteAsset ||
		value.ErrorCode != "" && value.ErrorCode != domain.PortfolioErrorIncomplete && value.ErrorCode != domain.PortfolioErrorMismatch {
		return false
	}
	for _, amount := range []decimal.Decimal{value.Opening.Quantity, value.Opening.CashQuote, value.Opening.CostQuote, value.Values.Quantity, value.Values.CashQuote, value.Values.CostQuote, value.Values.FeesQuote, value.Values.RealizedPnlQuote.Abs()} {
		if !domain.FixedDecimal(amount, 40, 36, true) {
			return false
		}
	}
	return !value.Opening.Quantity.IsZero() || value.Opening.CostQuote.IsZero()
}

func adminPortfolioBalance(row *modelv1.SimulatorBalance) (*domain.AdminPortfolioBalance, error) {
	if row == nil {
		return nil, nil
	}
	available, err := domain.ParseLedgerDecimal(row.Available, false, true)
	if err != nil {
		return nil, err
	}
	frozen, err := domain.ParseLedgerDecimal(row.Frozen, false, true)
	if err != nil {
		return nil, err
	}
	if row.Id <= 0 || row.Revision <= 0 || row.UpdateAt <= 0 || !coreIdentifier(row.Asset, 32) {
		return nil, domain.ErrAdminUnavailable
	}
	return &domain.AdminPortfolioBalance{
		ID:        row.Id,
		Asset:     row.Asset,
		Available: available,
		Frozen:    frozen,
		Revision:  row.Revision,
		UpdatedAt: time.UnixMilli(row.UpdateAt).UTC(),
	}, nil
}
