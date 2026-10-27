package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type submitFingerprint struct {
	AccountID     string
	StrategyID    int64
	ConfigVersion int64
	InstrumentID  int64
	Side          domain.Side
	Price         string
	Quantity      string
	TimeInForce   domain.TimeInForce
	ReduceOnly    bool
}

func (s *simulator) Submit(ctx context.Context, command domain.GatewaySubmit) (domain.CoreOrder, error) {
	in := command.Input
	if err := validateCoreCommand(ctx, in.Authority, command.ExecutionAuthority, in.AccountID, command.ClientOrderID, in.IdempotencyKey, in.InstrumentID, in.Deadline); err != nil {
		return domain.CoreOrder{}, err
	}
	if !domain.FixedDecimal(in.Price, 20, 18, false) || !domain.FixedDecimal(in.Quantity, 20, 18, false) || (in.Side != domain.SideBuy && in.Side != domain.SideSell) {
		return domain.CoreOrder{}, domain.ErrInvalidOrder
	}
	if (in.TimeInForce != domain.TimeInForceGTC && in.TimeInForce != domain.TimeInForcePostOnly) || in.ReduceOnly && in.Side != domain.SideSell {
		return domain.CoreOrder{}, domain.ErrUnsupportedOrder
	}
	fingerprint, err := fingerprintSubmit(in)
	if err != nil {
		return domain.CoreOrder{}, coreFailure(ctx, err)
	}
	var result domain.CoreOrder
	err = s.db.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Transaction(func(tx *gorm.DB) error {
		rows, err := lockGateway(ctx, tx, in.Authority, command.ExecutionAuthority, in.AccountID, in.InstrumentID)
		if err != nil {
			return err
		}
		existing, err := existingCommand(ctx, tx, in.AccountID, in.IdempotencyKey)
		if err == nil {
			if existing.Action != domain.GatewayActionSubmit || existing.ClientOrderId != command.ClientOrderID || existing.Fingerprint != fingerprint {
				return domain.ErrIdempotencyConflict
			}
			order, err := findCoreOrder(ctx, tx, in.AccountID, command.ClientOrderID)
			if err != nil {
				return err
			}
			result, err = coreOrder(order)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := validateInstrument(in, rows); err != nil {
			return err
		}
		if err := preventSelfCross(ctx, tx, in, rows.account.SubjectId); err != nil {
			return err
		}
		order, err := stageCoreSubmission(ctx, tx, command, rows, fingerprint)
		if err != nil {
			return err
		}
		result, err = coreOrder(order)
		return err
	})
	if err != nil {
		return domain.CoreOrder{}, coreFailure(ctx, err)
	}
	return result, nil
}

func fingerprintSubmit(in domain.OrderInput) (string, error) {
	data, err := json.Marshal(submitFingerprint{
		AccountID:     in.AccountID,
		StrategyID:    in.Authority.StrategyID,
		ConfigVersion: in.Authority.ConfigVersion,
		InstrumentID:  in.InstrumentID,
		Side:          in.Side,
		Price:         in.Price.String(),
		Quantity:      in.Quantity.String(),
		TimeInForce:   in.TimeInForce,
		ReduceOnly:    in.ReduceOnly,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func existingCommand(ctx context.Context, tx *gorm.DB, account, key string) (*modelv1.SimulatorCommand, error) {
	var row modelv1.SimulatorCommand
	err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Where("account_id = ? AND command_key = ? AND delete_at IS NULL", account, key).First(&row).Error
	return &row, err
}

func validateInstrument(in domain.OrderInput, rows coreRows) error {
	if !rows.account.Enabled || rows.instrument.ExecutionVenue != domain.ExecutionVenueSimulator || rows.instrument.Status != domain.InstrumentStatusTrading {
		return domain.ErrInvalidOrder
	}
	tick, err := decimal.NewFromString(rows.instrument.PriceTick)
	if err != nil {
		return err
	}
	step, err := decimal.NewFromString(rows.instrument.QuantityStep)
	if err != nil {
		return err
	}
	minimum, err := decimal.NewFromString(rows.instrument.MinNotional)
	if err != nil {
		return err
	}
	if !in.Price.Mod(tick).IsZero() || !in.Quantity.Mod(step).IsZero() || in.Price.Mul(in.Quantity).LessThan(minimum) {
		return domain.ErrInvalidOrder
	}
	return nil
}

func preventSelfCross(ctx context.Context, tx *gorm.DB, in domain.OrderInput, subject string) error {
	accounts := tx.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Select("account_id").Where("subject_id = ?", subject)
	query := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("account_id IN (?) AND instrument_id = ? AND state IN ? AND delete_at IS NULL", accounts, in.InstrumentID, []string{string(domain.OrderStateLive), string(domain.OrderStatePartiallyFilled)})
	if in.Side == domain.SideBuy {
		query = query.Where("side = ? AND price <= ?", string(domain.SideSell), in.Price.String())
	} else {
		query = query.Where("side = ? AND price >= ?", string(domain.SideBuy), in.Price.String())
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return domain.ErrSelfTrade
	}
	// This simulator uses explicit external-fill events. It must not claim
	// post-only acceptance when another resting core order would be crossed,
	// or silently leave a marketable GTC order unmatched.
	book := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Where("instrument_id = ? AND state IN ? AND delete_at IS NULL", in.InstrumentID, []string{string(domain.OrderStateLive), string(domain.OrderStatePartiallyFilled)})
	if in.Side == domain.SideBuy {
		book = book.Where("side = ? AND price <= ?", string(domain.SideSell), in.Price.String())
	} else {
		book = book.Where("side = ? AND price >= ?", string(domain.SideBuy), in.Price.String())
	}
	if err := book.Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		if in.TimeInForce == domain.TimeInForcePostOnly {
			return domain.ErrInvalidOrder
		}
		return domain.ErrUnsupportedOrder
	}
	return nil
}

func stageCoreSubmission(ctx context.Context, tx *gorm.DB, command domain.GatewaySubmit, rows coreRows, fingerprint string) (*modelv1.SimulatorOrder, error) {
	in := command.Input
	feeRate, err := decimal.NewFromString(rows.account.QuoteFeeRate)
	if err != nil {
		return nil, err
	}
	reserve, err := domain.SpotReserve(in.Side, in.Price, in.Quantity, feeRate)
	if err != nil {
		return nil, err
	}
	base, quote, err := lockBalances(ctx, tx, rows)
	if err != nil {
		return nil, err
	}
	funding := base
	if in.Side == domain.SideBuy {
		funding = quote
	}
	if err := adjustBalance(ctx, tx, funding, reserve.Neg(), reserve); err != nil {
		return nil, err
	}
	record := gatewayCommand(in.Authority, command.ExecutionAuthority, in.AccountID, command.ClientOrderID, in.IdempotencyKey, in.InstrumentID, in.Deadline.UnixMilli(), domain.GatewayActionSubmit, fingerprint)
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorCommand{}).Create(record).Error; err != nil {
		return nil, err
	}
	order := &modelv1.SimulatorOrder{
		AccountId:          in.AccountID,
		ClientOrderId:      command.ClientOrderID,
		InitialCommandId:   record.Id,
		LastCommandId:      record.Id,
		StrategyInstanceId: in.Authority.StrategyID,
		ConfigVersion:      in.Authority.ConfigVersion,
		InstrumentId:       in.InstrumentID,
		Side:               string(in.Side),
		Price:              in.Price.String(),
		Quantity:           in.Quantity.String(),
		FilledQuantity:     "0",
		TimeInForce:        string(in.TimeInForce),
		ReduceOnly:         in.ReduceOnly,
		State:              string(domain.OrderStateLive),
		CancelState:        string(domain.CancelStateNone),
		FrozenAsset:        funding.Asset,
		FrozenAmount:       reserve.String(),
		QuoteFeeRate:       feeRate.String(),
		Revision:           1,
	}
	if err := tx.WithContext(ctx).Model(&modelv1.SimulatorOrder{}).Create(order).Error; err != nil {
		return nil, err
	}
	return order, advanceCoreAccount(ctx, tx, rows.account)
}

func gatewayCommand(authority domain.StrategyAuthority, execution domain.LeaseToken, account, client, key string, instrumentID, deadline int64, action, fingerprint string) *modelv1.SimulatorCommand {
	return &modelv1.SimulatorCommand{
		AccountId:          account,
		CommandKey:         key,
		Action:             action,
		ClientOrderId:      client,
		StrategyInstanceId: authority.StrategyID,
		ConfigVersion:      authority.ConfigVersion,
		OwnerId:            authority.OwnerID,
		OwnerEpoch:         authority.OwnerEpoch,
		ExecutionOwnerId:   execution.OwnerID,
		ExecutionEpoch:     execution.Epoch,
		InstrumentId:       instrumentID,
		Fingerprint:        fingerprint,
		DeadlineAt:         deadline,
	}
}
