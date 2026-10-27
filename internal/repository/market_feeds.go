package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

type marketFeeds struct{ db *gorm.DB }

type marketStorageError struct{ cause error }

func (*marketStorageError) Error() string   { return domain.MessageMarketFeedStorage }
func (e *marketStorageError) Unwrap() error { return e.cause }

// NewMarketFeeds borrows a database, without opening resources or work.
func NewMarketFeeds(db *gorm.DB) (domain.MarketFeedRepository, error) {
	if db == nil {
		return nil, domain.ErrMarketIdentity
	}
	return &marketFeeds{db: db}, nil
}

func (r *marketFeeds) Create(ctx context.Context, input domain.MarketFeedDefinition) (domain.MarketFeedDefinition, error) {
	if err := marketCall(ctx); err != nil {
		return domain.MarketFeedDefinition{}, err
	}
	if err := input.Validate(); err != nil {
		return domain.MarketFeedDefinition{}, err
	}
	if input.ID != 0 {
		return domain.MarketFeedDefinition{}, domain.ErrMarketIdentity
	}
	var result domain.MarketFeedDefinition
	err := r.db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Transaction(func(tx *gorm.DB) error {
		var instrument modelv1.Instrument
		if err := tx.WithContext(ctx).Model(&modelv1.Instrument{}).Where("id = ? AND delete_at IS NULL", input.InstrumentID).
			Clauses(clause.Locking{Strength: "SHARE"}).First(&instrument).Error; err != nil {
			return err
		}
		expectedSymbol := instrument.BaseAsset + instrument.QuoteAsset
		if input.Source == domain.MarketSourceOKX {
			expectedSymbol = instrument.BaseAsset + "-" + instrument.QuoteAsset
		}
		if input.Symbol != expectedSymbol {
			return domain.ErrMarketIdentity
		}
		row := &modelv1.MarketFeed{
			Source:         string(input.Source),
			InstrumentId:   input.InstrumentID,
			ExternalSymbol: input.Symbol,
			Mode:           string(input.Mode),
			SourceVersion:  input.SourceVersion,
			Policy:         marketPolicyModel(input.Policy),
		}
		if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
			return err
		}
		var saved modelv1.MarketFeed
		if err := tx.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", input.Source, input.InstrumentID, input.Mode).First(&saved).Error; err != nil {
			return err
		}
		var err error
		result, err = marketFeedValue(&saved)
		if err != nil {
			return err
		}
		if !sameMarketFeed(input, result) {
			return domain.ErrMarketFeedConflict
		}
		return nil
	})
	if err != nil {
		return domain.MarketFeedDefinition{}, marketFailure(ctx, err)
	}
	return result, nil
}

func (r *marketFeeds) Get(ctx context.Context, id int64) (domain.MarketFeedDefinition, error) {
	if err := marketCall(ctx); err != nil {
		return domain.MarketFeedDefinition{}, err
	}
	if id <= 0 {
		return domain.MarketFeedDefinition{}, domain.ErrMarketIdentity
	}
	var row modelv1.MarketFeed
	if err := r.db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("id = ? AND delete_at IS NULL", id).First(&row).Error; err != nil {
		return domain.MarketFeedDefinition{}, marketFailure(ctx, err)
	}
	return marketFeedValue(&row)
}

func (r *marketFeeds) Find(ctx context.Context, source domain.MarketSource, instrumentID int64, mode domain.MarketMode) (domain.MarketFeedDefinition, error) {
	if err := marketCall(ctx); err != nil {
		return domain.MarketFeedDefinition{}, err
	}
	if source.ValidateSelection() != nil || instrumentID <= 0 || (mode != domain.MarketModeLive && mode != domain.MarketModeReplay) {
		return domain.MarketFeedDefinition{}, domain.ErrMarketIdentity
	}
	var row modelv1.MarketFeed
	if err := r.db.WithContext(ctx).Model(&modelv1.MarketFeed{}).Where("source = ? AND instrument_id = ? AND mode = ? AND delete_at IS NULL", source, instrumentID, mode).First(&row).Error; err != nil {
		return domain.MarketFeedDefinition{}, marketFailure(ctx, err)
	}
	return marketFeedValue(&row)
}

func marketFeedValue(row *modelv1.MarketFeed) (domain.MarketFeedDefinition, error) {
	policy, err := marketPolicyValue(row.Policy)
	if err != nil {
		return domain.MarketFeedDefinition{}, err
	}
	result := domain.MarketFeedDefinition{
		ID:            row.Id,
		Source:        domain.MarketSource(row.Source),
		InstrumentID:  row.InstrumentId,
		Symbol:        row.ExternalSymbol,
		Mode:          domain.MarketMode(row.Mode),
		SourceVersion: row.SourceVersion,
		Policy:        policy,
	}
	return result, result.Validate()
}

func marketPolicyValue(raw *commonv1.MarketPolicy) (domain.MarketPolicy, error) {
	if raw == nil || len(raw.ProtoReflect().GetUnknown()) != 0 || raw.MaximumAgeMs <= 0 || raw.MaximumAgeMs > 5000 || raw.MaximumSkewMs < 0 || raw.MaximumSkewMs > 1000 || raw.RecoveryWindowMs < 0 || raw.RecoveryWindowMs > 5000 {
		return domain.MarketPolicy{}, domain.ErrUnreliableMarket
	}
	rule := domain.DecimalRule{
		IntegerDigits: 5,
		Scale:         18,
	}
	spread, err := domain.ParseDecimal(raw.MaximumSpreadBps, rule)
	if err != nil {
		return domain.MarketPolicy{}, err
	}
	jump, err := domain.ParseDecimal(raw.MaximumJumpBps, rule)
	if err != nil {
		return domain.MarketPolicy{}, err
	}
	policy := domain.MarketPolicy{
		MaximumAge:      time.Duration(raw.MaximumAgeMs) * time.Millisecond,
		MaximumSkew:     time.Duration(raw.MaximumSkewMs) * time.Millisecond,
		MaximumSpreadBP: spread,
		MaximumJumpBP:   jump,
		RecoverySamples: int(raw.RecoverySamples),
		RecoveryWindow:  time.Duration(raw.RecoveryWindowMs) * time.Millisecond,
	}
	return policy, policy.Validate()
}

func marketPolicyModel(policy domain.MarketPolicy) *commonv1.MarketPolicy {
	return &commonv1.MarketPolicy{
		MaximumAgeMs:     policy.MaximumAge.Milliseconds(),
		MaximumSkewMs:    policy.MaximumSkew.Milliseconds(),
		MaximumSpreadBps: policy.MaximumSpreadBP.String(),
		MaximumJumpBps:   policy.MaximumJumpBP.String(),
		RecoverySamples:  int32(policy.RecoverySamples),
		RecoveryWindowMs: policy.RecoveryWindow.Milliseconds(),
	}
}

func sameMarketFeed(a, b domain.MarketFeedDefinition) bool {
	return a.Source == b.Source && a.InstrumentID == b.InstrumentID && a.Symbol == b.Symbol && a.Mode == b.Mode && a.SourceVersion == b.SourceVersion &&
		a.Policy.MaximumAge == b.Policy.MaximumAge && a.Policy.MaximumSkew == b.Policy.MaximumSkew &&
		a.Policy.MaximumSpreadBP.Equal(b.Policy.MaximumSpreadBP) && a.Policy.MaximumJumpBP.Equal(b.Policy.MaximumJumpBP) &&
		a.Policy.RecoverySamples == b.Policy.RecoverySamples && a.Policy.RecoveryWindow == b.Policy.RecoveryWindow
}

func marketCall(ctx context.Context) error {
	if ctx == nil {
		return domain.ErrMarketIdentity
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.ErrMarketIdentity
	}
	return ctx.Err()
}

func marketFailure(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, domain.ErrMarketFeedConflict) || errors.Is(err, domain.ErrUnreliableMarket) || errors.Is(err, domain.ErrMarketIdentity) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrMarketFeedMissing
	}
	return &marketStorageError{cause: errors.Join(err, ctx.Err())}
}
