package repository

import (
	"context"

	"github.com/hmlax/muskdeer/deer/ownerid"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

func lifecycleBoundary(ctx context.Context, strategyID int64, token domain.LeaseToken, transition domain.StrategyTransition) error {
	if err := coreContext(ctx); err != nil {
		return err
	}
	if strategyID <= 0 || token.Key.Kind != domain.LeaseKindStrategy || token.Key.StrategyID != strategyID || token.Key.AccountID != "" ||
		token.Key.MarketFeedID != 0 || token.Epoch <= 0 || !ownerid.Valid(token.OwnerID) {
		return domain.ErrInvalidOrder
	}
	switch transition.State {
	case domain.StrategyStateRecovering, domain.StrategyStateRunning, domain.StrategyStatePausing, domain.StrategyStatePaused,
		domain.StrategyStateStopping, domain.StrategyStateStopped, domain.StrategyStateError:
	default:
		return domain.ErrInvalidOrder
	}
	if transition.ActivateRequested && transition.State != domain.StrategyStateRunning && transition.State != domain.StrategyStateRecovering {
		return domain.ErrInvalidOrder
	}
	return nil
}

func lifecycleDesired(row *modelv1.StrategyInstance, transition domain.StrategyTransition) error {
	if transition.State == domain.StrategyStateRunning && !row.DesiredRunning {
		return domain.ErrRiskDenied
	}
	switch transition.State {
	case domain.StrategyStatePausing, domain.StrategyStatePaused, domain.StrategyStateStopping, domain.StrategyStateStopped:
		if row.DesiredRunning {
			return domain.ErrRiskDenied
		}
	}
	return nil
}

func lifecycleUnchanged(row *modelv1.StrategyInstance, token domain.LeaseToken, transition domain.StrategyTransition) bool {
	return row.RuntimeOwnerId == token.OwnerID && row.RuntimeOwnerEpoch == token.Epoch && row.State == string(transition.State) &&
		(!transition.ActivateRequested || row.GetActiveConfigVersion() == row.RequestedConfigVersion && row.GetActiveMarketDataSource() == row.MarketDataSource)
}

func strategySpec(row *modelv1.StrategyInstance) domain.StrategySpec {
	return domain.StrategySpec{
		ID:                row.Id,
		Key:               row.InstanceKey,
		AccountID:         row.AccountId,
		InstrumentID:      row.InstrumentId,
		Template:          domain.StrategyTemplate(row.Template),
		CodeVersion:       row.CodeVersion,
		Revision:          row.Revision,
		State:             domain.StrategyState(row.State),
		DesiredRunning:    row.DesiredRunning,
		RequestedVersion:  row.RequestedConfigVersion,
		RequestedSource:   domain.MarketSource(row.MarketDataSource),
		ActiveVersion:     row.GetActiveConfigVersion(),
		ActiveSource:      domain.MarketSource(row.GetActiveMarketDataSource()),
		Mode:              domain.MarketMode(row.MarketMode),
		RuntimeOwnerID:    row.RuntimeOwnerId,
		RuntimeOwnerEpoch: row.RuntimeOwnerEpoch,
	}
}
