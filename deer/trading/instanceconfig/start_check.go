package instanceconfig

import (
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
)

// Start-check failures never change desired state or append control history.
var (
	ErrStartCheckExpired = errors.New(domain.MakerCheckExpiredMessage)
	ErrMakerBudget       = errors.New(domain.MakerBudgetMessage)
)

// StartCheck pins a trusted console calculation to its policy and input expiry.
// No external RPC runs while the management transaction holds financial locks.
type StartCheck struct {
	PolicyUpdatedAt  int64
	Profile          *common.VenueProfilePolicy
	RequiredNotional string
	ValidUntilMs     int64
}

func validateStartCheck(budget *model.TradingAccountPolicy, proof *StartCheck, now int64) error {
	current, err := venueconfig.ProfileFromBudget(budget)
	expected := proto.CloneOf(proof.Profile)
	if expected == nil {
		return ErrConflict
	}
	expected.StrategyInstanceId = 0
	if err != nil || budget.UpdateAt != proof.PolicyUpdatedAt || !proto.Equal(current, expected) {
		return ErrConflict
	}
	if proof.ValidUntilMs <= now {
		return ErrStartCheckExpired
	}
	required, e1 := domain.ParseLedgerDecimal(proof.RequiredNotional, false, false)
	maximum, e2 := domain.ParseLedgerDecimal(budget.MaximumNotional, false, false)
	if e1 != nil || e2 != nil {
		return ErrInvalid
	}
	if required.GreaterThan(maximum) {
		return ErrMakerBudget
	}
	return nil
}

func validateManagementStart(row *strategy.StrategyInstance, budget *model.TradingAccountPolicy, command ManagementWrite) error {
	if command.Operation != ManagementStart || command.StartCheck == nil {
		return nil
	}
	if command.StartCheck.Profile.GetStrategyInstanceId() != row.Id {
		return ErrConflict
	}
	return validateStartCheck(budget, command.StartCheck, time.Now().UnixMilli())
}
