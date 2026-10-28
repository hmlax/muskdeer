//go:build integration

package migrate

import (
	"context"
	"database/sql"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const (
	soakBaseAsset    = "BTC"
	soakQuoteAsset   = "USDT"
	soakRecordSample = "sample"
	soakRecordResult = "result"
)

type soakSample struct {
	RecordKind       string `json:"record_kind"`
	ElapsedMS        int64  `json:"elapsed_ms"`
	FillCount        int64  `json:"fill_count"`
	ProjectedFills   int64  `json:"projected_fills"`
	CoreOrders       int64  `json:"core_orders"`
	QuoteBuys        int64  `json:"quote_buys"`
	QuoteSells       int64  `json:"quote_sells"`
	BaseTotal        string `json:"base_total"`
	QuoteTotal       string `json:"quote_total"`
	BaseFrozen       string `json:"base_frozen"`
	QuoteFrozen      string `json:"quote_frozen"`
	ReservedNotional string `json:"reserved_notional"`
	WorstBase        string `json:"worst_base"`
	FeesQuote        string `json:"fees_quote"`
	CostQuote        string `json:"cost_quote"`
	RealizedPnlQuote string `json:"realized_pnl_quote"`
	Reconciled       bool   `json:"reconciled"`
	RiskFlag         bool   `json:"risk_flag"`
	RiskReason       string `json:"risk_reason"`
}

type soakAmount struct{ Amount string }

func readSoakSample(ctx context.Context, db *gorm.DB, submit domain.GatewaySubmit) (soakSample, error) {
	var sample soakSample
	err := db.WithContext(ctx).Model(&modelv1.AccountBudget{}).Transaction(func(tx *gorm.DB) error {
		var balances []*modelv1.SimulatorBalance
		if err := tx.WithContext(ctx).Model(&modelv1.SimulatorBalance{}).Where("account_id = ? AND delete_at IS NULL", submit.Input.AccountID).Find(&balances).Error; err != nil {
			return err
		}
		var base, quote, baseFrozen, quoteFrozen decimal.Decimal
		if len(balances) != 2 {
			return domain.ErrInvalidOrder
		}
		for _, balance := range balances {
			available, err := domain.ParseLedgerDecimal(balance.Available, false, true)
			if err != nil {
				return err
			}
			frozen, err := domain.ParseLedgerDecimal(balance.Frozen, false, true)
			if err != nil {
				return err
			}
			switch balance.Asset {
			case soakBaseAsset:
				base, baseFrozen = available.Add(frozen), frozen
			case soakQuoteAsset:
				quote, quoteFrozen = available.Add(frozen), frozen
			default:
				return domain.ErrInvalidOrder
			}
		}
		if err := checkSoakFrozen(ctx, tx, submit.Input.AccountID, &sample, baseFrozen, quoteFrozen); err != nil {
			return err
		}
		var sum soakAmount
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Where("account_id = ?", submit.Input.AccountID).Select("COALESCE(sum(reserved_quote_notional), 0)::text AS amount").Scan(&sum).Error; err != nil {
			return err
		}
		reserved, err := domain.ParseLedgerDecimal(sum.Amount, false, true)
		if err != nil {
			return err
		}
		if err := checkSoakBudgets(ctx, tx, submit, reserved); err != nil {
			return err
		}
		var buys soakAmount
		if err := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).
			Where("account_id = ? AND side = ? AND state IN ? AND delete_at IS NULL", submit.Input.AccountID, domain.SideBuy, []domain.OrderState{domain.OrderStateIntent, domain.OrderStateSubmitting, domain.OrderStateUnknown, domain.OrderStateLive, domain.OrderStatePartiallyFilled}).
			Select("COALESCE(sum(quantity - filled_quantity), 0)::text AS amount").Scan(&buys).Error; err != nil {
			return err
		}
		remainingBuys, err := domain.ParseLedgerDecimal(buys.Amount, false, true)
		if err != nil {
			return err
		}
		worst := base.Add(remainingBuys)
		if worst.GreaterThan(decimal.NewFromInt(20)) {
			return domain.ErrRiskDenied
		}
		sample.BaseTotal, sample.QuoteTotal = base.String(), quote.String()
		sample.BaseFrozen, sample.QuoteFrozen = baseFrozen.String(), quoteFrozen.String()
		sample.ReservedNotional, sample.WorstBase = reserved.String(), worst.String()
		return readSoakProjection(ctx, tx, submit, &sample, base, quote)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return sample, err
}

func checkSoakFrozen(ctx context.Context, tx *gorm.DB, account string, sample *soakSample, baseFrozen, quoteFrozen decimal.Decimal) error {
	var orders []*modelv1.SimulatorOrder
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND state IN ? AND delete_at IS NULL", account, []domain.OrderState{domain.OrderStateLive, domain.OrderStatePartiallyFilled}).Find(&orders).Error; err != nil {
		return err
	}
	var base, quote decimal.Decimal
	for _, order := range orders {
		frozen, err := domain.ParseLedgerDecimal(order.FrozenAmount, false, true)
		if err != nil {
			return err
		}
		switch order.FrozenAsset {
		case soakBaseAsset:
			base = base.Add(frozen)
		case soakQuoteAsset:
			quote = quote.Add(frozen)
		default:
			return domain.ErrInvalidOrder
		}
		if order.CancelState == string(domain.CancelStateNone) {
			switch domain.Side(order.Side) {
			case domain.SideBuy:
				sample.QuoteBuys++
			case domain.SideSell:
				sample.QuoteSells++
			default:
				return domain.ErrInvalidOrder
			}
		}
	}
	if !base.Equal(baseFrozen) || !quote.Equal(quoteFrozen) {
		return domain.ErrReconciliationMismatch
	}
	intents := tx.WithContext(ctx).Model(&modelv1.OrderIntent{}).Select("client_order_id").Where("account_id = ?", account)
	var unexplained int64
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ? AND client_order_id NOT IN (?)", account, intents).Count(&unexplained).Error; err != nil {
		return err
	}
	if unexplained != 0 {
		return domain.ErrReconciliationMismatch
	}
	return tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id = ?", account).Count(&sample.CoreOrders).Error
}

func checkSoakBudgets(ctx context.Context, tx *gorm.DB, submit domain.GatewaySubmit, expected decimal.Decimal) error {
	var account modelv1.AccountBudget
	if err := tx.WithContext(ctx).Model(&modelv1.AccountBudget{}).Where("account_id = ?", submit.Input.AccountID).First(&account).Error; err != nil {
		return err
	}
	var strategy modelv1.StrategyBudget
	if err := tx.WithContext(ctx).Model(&modelv1.StrategyBudget{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&strategy).Error; err != nil {
		return err
	}
	var global []*modelv1.RiskBudget
	if err := tx.WithContext(ctx).Model(&modelv1.RiskBudget{}).Find(&global).Error; err != nil {
		return err
	}
	if len(global) != 2 {
		return domain.ErrRiskDenied
	}
	budgets := [][2]string{{account.ReservedNotional, account.LimitNotional}, {strategy.ReservedNotional, strategy.LimitNotional}}
	for _, row := range global {
		budgets = append(budgets, [2]string{row.ReservedNotional, row.LimitNotional})
	}
	for _, pair := range budgets {
		reserved, err := domain.ParseLedgerDecimal(pair[0], false, true)
		if err != nil {
			return err
		}
		limit, err := domain.ParseLedgerDecimal(pair[1], false, false)
		if err != nil {
			return err
		}
		if !reserved.Equal(expected) || reserved.GreaterThan(limit) {
			return domain.ErrRiskDenied
		}
	}
	return nil
}

func readSoakProjection(ctx context.Context, tx *gorm.DB, submit domain.GatewaySubmit, sample *soakSample, base, quote decimal.Decimal) error {
	orders := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Select("id").Where("account_id = ?", submit.Input.AccountID)
	var source struct {
		Count int64
		Last  int64
	}
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorFill{}).Where("core_order_id IN (?)", orders).Select("count(*) AS count, COALESCE(max(id), 0) AS last").Scan(&source).Error; err != nil {
		return err
	}
	sample.FillCount = source.Count
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioFill{}).Where("account_id = ?", submit.Input.AccountID).Count(&sample.ProjectedFills).Error; err != nil {
		return err
	}
	var portfolio modelv1.PortfolioAccount
	if err := tx.WithContext(ctx).Model(&modelv1.PortfolioAccount{}).Where("account_id = ?", submit.Input.AccountID).First(&portfolio).Error; err != nil {
		return err
	}
	sample.Reconciled = portfolio.Reconciled && portfolio.LastSourceSequence == source.Last && sample.ProjectedFills == source.Count
	sample.FeesQuote, sample.CostQuote, sample.RealizedPnlQuote = portfolio.FeesQuote, portfolio.CostQuote, portfolio.RealizedPnlQuote
	if sample.Reconciled && (!decimal.RequireFromString(portfolio.Quantity).Equal(base) || !decimal.RequireFromString(portfolio.CashQuote).Equal(quote)) {
		return domain.ErrReconciliationMismatch
	}
	var risk modelv1.RiskSnapshot
	if err := tx.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("strategy_instance_id = ?", submit.Input.Authority.StrategyID).First(&risk).Error; err != nil {
		return err
	}
	sample.RiskFlag, sample.RiskReason = risk.AllowNewRisk, risk.ReasonCode
	if sample.RiskFlag && (!risk.Reconciled || !sample.Reconciled) {
		return domain.ErrReconciliationMismatch
	}
	return nil
}
