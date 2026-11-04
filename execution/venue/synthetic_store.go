package venue

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

// PublishSynthetic consumes a durable, finite send slot before its SDK effect.
// An unknown outcome consumes its quota and is never sent again.
func (s *Store) PublishSynthetic(ctx context.Context, execution domain.LeaseToken, cfg *commonv1.HimalayaPolicy, plan *commonv1.SyntheticTapePlan, send func(context.Context, *gateway.SyntheticRequest) (*gateway.SyntheticResult, error)) (*commonv1.SyntheticTapeResult, error) {
	if !validStoreContext(ctx) || send == nil || !validSyntheticPlan(cfg, execution, plan) {
		return nil, ErrInvalidSubmit
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(encoded))
	row, fresh, err := s.claimSynthetic(ctx, execution, cfg, plan, digest)
	if err != nil {
		return nil, err
	}
	if !fresh {
		cached := proto.CloneOf(row.Result)
		cached.Replayed = true
		return cached, nil
	}
	// The claim is committed before the SDK callback; this transaction retains
	// the execution, strategy and source authority rows until the bounded,
	// single attempt completes.
	var response *gateway.SyntheticResult
	err = s.db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Transaction(func(tx *gorm.DB) error {
		_, left, err := lockSyntheticAuthorities(ctx, tx, execution, row.Plan)
		if err != nil {
			return err
		}
		var persisted executionmodelv1.SyntheticTapeIntent
		if err := tx.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Where("id = ?", row.Id).First(&persisted).Error; err != nil {
			return err
		}
		if persisted.PolicyDigest != digest || persisted.Result.GetState() != native.SyntheticUnknown || !proto.Equal(persisted.Plan, row.Plan) {
			return ErrInvalidSubmit
		}
		bounded, cancel := context.WithTimeout(ctx, left)
		defer cancel()
		side := domain.SideBuy
		if row.Plan.Side == string(domain.SideSell) {
			side = domain.SideSell
		}
		response, err = send(bounded, &gateway.SyntheticRequest{
			Symbol:   row.Symbol,
			Side:     side,
			Price:    row.Plan.Price,
			Quantity: row.Plan.Quantity,
		})
		return err
	})
	if err != nil || !validSyntheticReceipt(row.Plan, response) {
		return proto.CloneOf(row.Result), nil
	}
	row.Result = &commonv1.SyntheticTapeResult{
		State:       native.SyntheticPublished,
		TradeId:     response.TradeID,
		Price:       response.Price,
		Quantity:    response.Quantity,
		TimestampMs: response.TimestampMS,
		SourceTrade: proto.CloneOf(row.Plan.SourceTrade),
	}
	if err := s.db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Where("id = ?", row.Id).Select("Result").Updates(row).Error; err != nil {
		return nil, err
	}
	return proto.CloneOf(row.Result), nil
}

func validSyntheticPlan(cfg *commonv1.HimalayaPolicy, execution domain.LeaseToken, plan *commonv1.SyntheticTapePlan) bool {
	if plan == nil || execution.Key.AccountID != native.SyntheticExecutionKey(plan.RunId, plan.Symbol) ||
		plan.GetStrategy().GetAccountId() != native.SyntheticStrategyKey(plan.RunId, plan.Symbol) ||
		plan.GetBook().GetPublisher().GetAccountId() != native.MarketKey(plan.Symbol) || plan.RunId != cfg.GetSyntheticTape().GetRunId() {
		return false
	}
	expected, err := native.SyntheticSourcePlan(cfg, plan)
	return err == nil && proto.Equal(expected, plan)
}

func (s *Store) claimSynthetic(ctx context.Context, execution domain.LeaseToken, cfg *commonv1.HimalayaPolicy, plan *commonv1.SyntheticTapePlan, digest string) (*executionmodelv1.SyntheticTapeIntent, bool, error) {
	var result *executionmodelv1.SyntheticTapeIntent
	fresh := false
	err := s.db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Transaction(func(tx *gorm.DB) error {
		_, _, err := lockSDKLease(ctx, tx, execution.Key.AccountID, execution.OwnerID, uint64(execution.Epoch))
		if err != nil {
			return err
		}
		var rows []*executionmodelv1.SyntheticTapeIntent
		if err := tx.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Where("run_id = ? AND symbol = ?", plan.RunId, plan.Symbol).Order("sequence").Limit(1001).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 1000 {
			return ErrRiskLimit
		}
		total := decimal.Zero
		last := int64(0)
		lastSource := int64(0)
		for i, row := range rows {
			amount, err := syntheticReservation(row, digest, int64(i))
			if err != nil {
				return err
			}
			total = total.Add(amount)
			if cfg.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
				sourceID, err := validateSyntheticTradeAudit(cfg, row.Plan)
				if err != nil || sourceID <= lastSource {
					return ErrInvalidSubmit
				}
				lastSource = sourceID
			} else if row.Plan.GetSourceTrade() != nil {
				return ErrInvalidSubmit
			}
			last = max(last, row.CreateAt)
			if row.Sequence == int64(plan.Sequence) {
				result = row
			}
		}
		if result != nil {
			// Returning a committed receipt has no SDK effect. Do not wait on
			// the strategy/source rows held by the first sender or require its
			// old publication authority to remain live merely to read a result.
			return nil
		}
		if cfg.SyntheticTape.Mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
			sourceID, err := native.TradeSequence(plan.SourceTrade)
			if err != nil || sourceID <= lastSource {
				return ErrInvalidSubmit
			}
		}
		now, _, err := lockSyntheticAuthorities(ctx, tx, execution, plan)
		if err != nil {
			return err
		}
		if plan.Sequence != uint32(len(rows)) {
			return ErrInvalidSubmit
		}
		if last > 0 && now-last < cfg.SyntheticTape.Interval.AsDuration().Milliseconds() {
			return ErrRiskLimit
		}
		price, _ := native.Value(plan.Price, false, false)
		quantity, _ := native.Value(plan.Quantity, false, false)
		notional := price.Mul(quantity)
		maximum, _ := native.Value(native.SyntheticProfile(cfg, plan.Symbol).MaximumNotional, false, false)
		if total.Add(notional).GreaterThan(maximum) {
			return ErrRiskLimit
		}
		result = &executionmodelv1.SyntheticTapeIntent{
			CreateAt:     now,
			UpdateAt:     now,
			RunId:        plan.RunId,
			Symbol:       plan.Symbol,
			Sequence:     int64(plan.Sequence),
			PolicyDigest: digest,
			Plan:         proto.CloneOf(plan),
			Notional:     notional.String(),
			Result: &commonv1.SyntheticTapeResult{
				State:       native.SyntheticUnknown,
				SourceTrade: proto.CloneOf(plan.SourceTrade),
			},
		}
		if err := tx.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Create(result).Error; err != nil {
			return err
		}
		fresh = true
		return nil
	})
	return result, fresh, err
}

// The already locked execution lease serializes source-event admission across
// owners. The durable plans retain a monotonic cursor, including unknown slots.
func validateSyntheticTradeAudit(cfg *commonv1.HimalayaPolicy, plan *commonv1.SyntheticTapePlan) (int64, error) {
	target, err := native.SyntheticTradeTarget(cfg, plan.GetSourceTrade())
	if err != nil || target.Side != plan.GetSide() || target.Price != plan.GetPrice() || target.Quantity != plan.GetQuantity() ||
		plan.SourceTrade.GetPublisher().GetAccountId() != native.TradeKey(plan.Symbol) {
		return 0, ErrInvalidSubmit
	}
	return native.TradeSequence(plan.SourceTrade)
}

func syntheticReservation(row *executionmodelv1.SyntheticTapeIntent, digest string, sequence int64) (decimal.Decimal, error) {
	amount, e1 := domain.ParseLedgerDecimal(row.Notional, false, false)
	price, e2 := native.Value(row.Plan.GetPrice(), false, false)
	quantity, e3 := native.Value(row.Plan.GetQuantity(), false, false)
	if e1 != nil || e2 != nil || e3 != nil || row.DeleteAt != nil || row.PolicyDigest != digest || row.Sequence != sequence || row.Plan.GetSequence() != uint32(sequence) ||
		row.Plan.GetRunId() != row.RunId || row.Plan.GetSymbol() != row.Symbol || !amount.Equal(price.Mul(quantity)) ||
		!proto.Equal(row.Plan.GetSourceTrade(), row.Result.GetSourceTrade()) ||
		row.Result.GetState() != native.SyntheticPublished && row.Result.GetState() != native.SyntheticUnknown {
		return decimal.Zero, ErrInvalidSubmit
	}
	return amount, nil
}

func lockSyntheticAuthorities(ctx context.Context, tx *gorm.DB, execution domain.LeaseToken, plan *commonv1.SyntheticTapePlan) (int64, time.Duration, error) {
	lease, now, err := lockSDKLease(ctx, tx, execution.Key.AccountID, execution.OwnerID, uint64(execution.Epoch))
	if err != nil {
		return 0, 0, err
	}
	remaining := time.Duration(lease.ExpiresAt-now)*time.Millisecond - 50*time.Millisecond
	source := proto.CloneOf(plan.Book.Publisher)
	source.ExpiresAtMs = plan.Book.ValidUntilMs
	for _, authority := range []*commonv1.HimalayaAuthority{plan.Strategy, source} {
		left, err := repository.LockNativeAuthority(ctx, tx, authority)
		if err != nil {
			return 0, 0, err
		}
		remaining = min(remaining, left)
	}
	if plan.SourceTrade != nil {
		tradeSource := proto.CloneOf(plan.SourceTrade.Publisher)
		tradeSource.ExpiresAtMs = plan.SourceTrade.ValidUntilMs
		left, err := repository.LockNativeAuthority(ctx, tx, tradeSource)
		if err != nil {
			return 0, 0, err
		}
		remaining = min(remaining, left)
	}
	if remaining <= 0 {
		return 0, 0, domain.ErrLeaseLost
	}
	return now, remaining, nil
}

func validSyntheticReceipt(plan *commonv1.SyntheticTapePlan, response *gateway.SyntheticResult) bool {
	if response.GetTradeID() == "" || response.GetTimestampMS() == 0 {
		return false
	}
	price, e1 := native.Value(response.Price, false, false)
	quantity, e2 := native.Value(response.Quantity, false, false)
	expectedPrice, e3 := native.Value(plan.Price, false, false)
	expectedQuantity, e4 := native.Value(plan.Quantity, false, false)
	return e1 == nil && e2 == nil && e3 == nil && e4 == nil && price.Equal(expectedPrice) && quantity.Equal(expectedQuantity)
}
