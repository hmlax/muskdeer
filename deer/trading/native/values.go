// Package native defines exact, shared values for the existing Himalaya SDK route.
package native

import (
	"errors"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
)

// Native runtime identifiers are persisted and shared across all four services.
const (
	Venue            = "himalaya_sdk"
	Source           = "binance"
	Segment          = "usdm"
	ClientPrefix     = "deer_"
	LegacyPrefix     = "mm_"
	StatusOpen       = "OPEN"
	StatusPartial    = "PARTIALLY_FILLED"
	StatusFilled     = "FILLED"
	StatusCanceled   = "CANCELED"
	StatusPartCancel = "PARTIALLY_CANCELED"
	StateRunning     = "RUNNING"
	StateRecovering  = "RECOVERING"
	BookAge          = 1500 * time.Millisecond
	SnapshotAge      = 2 * time.Second
	MessageInvalid   = "native trading observation is invalid or stale"
)

// ErrInvalid denies risk on missing, stale or mismatched observations.
var ErrInvalid = errors.New(MessageInvalid)

// Value parses plain exact business decimals at the SDK/service boundary.
func Value(value string, negative, zero bool) (decimal.Decimal, error) {
	return domain.ParseDecimal(value, domain.DecimalRule{
		IntegerDigits: 38,
		Scale:         18,
		AllowNegative: negative,
		AllowZero:     zero,
	})
}

// Active and Terminal classify only known upstream states.
func Active(state string) bool { return state == StatusOpen || state == StatusPartial }

// Terminal requires positive terminal evidence; absence is never terminal.
func Terminal(state string) bool {
	return state == StatusFilled || state == StatusCanceled || state == StatusPartCancel
}

// ScopeKey scopes execution to a whole fund, independent of instrument.
func ScopeKey(user, fund int64) string {
	return "himalaya/" + strconv.FormatInt(user, 10) + "/" + strconv.FormatInt(fund, 10)
}

// StrategyKey and MarketKey reuse retained execution-lease records in separate scopes.
func StrategyKey(user, fund int64) string { return "hs/" + ScopeKey(user, fund) }

// MarketKey identifies one selected futures instrument publisher.
func MarketKey(symbol string) string { return "hm/" + symbol }

// PortfolioKey identifies the fund's rebuildable observation publisher.
func PortfolioKey(user, fund int64) string { return "hp/" + ScopeKey(user, fund) }

// Symbol accepts a bounded exchange symbol without URL or routing syntax.
func Symbol(value string) bool {
	if len(value) < 2 || len(value) > 24 {
		return false
	}
	for _, c := range value {
		if c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

// Scope returns a fresh typed wire scope from an immutable profile.
func Scope(profile *commonv1.HimalayaProfilePolicy) *commonv1.HimalayaScope {
	return &commonv1.HimalayaScope{
		UserId: profile.UserId,
		FundId: profile.FundId,
		Symbol: profile.Symbol,
	}
}

// Profile rejects partial matches rather than implicitly using another account.
func Profile(cfg *commonv1.HimalayaPolicy, scope *commonv1.HimalayaScope) (*commonv1.HimalayaProfilePolicy, error) {
	for _, profile := range cfg.GetProfiles() {
		if profile.UserId == scope.GetUserId() && profile.FundId == scope.GetFundId() && profile.Symbol == scope.GetSymbol() {
			return profile, nil
		}
	}
	return nil, ErrInvalid
}
