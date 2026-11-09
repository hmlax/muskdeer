package venue

import (
	"context"
	"crypto/rand"
	"time"

	"google.golang.org/protobuf/proto"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	model "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
	"github.com/hmlax/muskdeer/execution/venue/gateway"
)

func (r *Runtime) directMaker() bool {
	return r.cfg.InstanceID > 0 && r.cfg.Template == domain.StrategyTemplateMarketMaker
}

func (r *Runtime) quoteCutover(configured bool) bool {
	return configured || r.directMaker() && r.cutover.Load()
}

func (r *Runtime) submitDirectQuote(ctx context.Context, cycle *quoteCycle, target *common.VenueTarget) error {
	client, ok := r.client.Load().Client.(gateway.QuoteClient)
	if !ok || !validStoreContext(ctx) {
		return ErrInvalidConfig
	}
	price, e1 := native.Value(target.GetPrice(), false, false)
	quantity, e2 := native.Value(target.GetQuantity(), false, false)
	if e1 != nil || e2 != nil || cycle.authority.InstanceID != r.cfg.InstanceID || cycle.authority.Revision != r.cfg.Revision {
		return ErrInvalidSubmit
	}
	if err := r.checkDirectQuoteConfig(ctx, cycle.profile); err != nil {
		return err
	}
	request := gateway.QuoteRequest{
		Scope: Scope{
			UserID: cycle.profile.UserId,
			FundID: cycle.profile.FundId,
			Symbol: cycle.profile.Symbol,
		},
		ClientID: native.InstanceClientPrefix(cycle.profile) + rand.Text(),
		Side:     domain.Side(target.Side),
		Price:    price,
		Quantity: quantity,
		Leverage: cycle.profile.Leverage,
	}
	if err := r.checkDirectQuoteBudget(ctx, cycle, request); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	deadline, _ := ctx.Deadline()
	left := time.Duration(min(cycle.authority.Book.GetValidUntilMs(), cycle.snapshot.GetValidUntilMs(), deadline.UnixMilli())-now)*time.Millisecond - 50*time.Millisecond
	if left < minimumLiveSDKWindow {
		return native.ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, min(left, maximumOperationTimeout))
	defer cancel()
	if _, err := client.SubmitQuote(bounded, request); err != nil {
		return err
	}
	cycle.result.Placed++
	cycle.admitted = append(cycle.admitted, target)
	return nil
}

// Read current configuration only. No order transaction, lease or journal is
// created or held across the external operation.
func (r *Runtime) checkDirectQuoteConfig(ctx context.Context, profile *common.VenueProfilePolicy) error {
	var active int64
	err := r.db.WithContext(ctx).Model(&model.StrategyInstance{}).
		Joins("JOIN strategy_templates AS template ON template.id = strategy_instances.template_id AND template.revision = strategy_instances.template_revision AND template.enabled AND template.delete_at IS NULL").
		Joins("JOIN instruments AS instrument ON instrument.id = strategy_instances.instrument_id AND instrument.delete_at IS NULL").
		Where("strategy_instances.id = ? AND strategy_instances.revision = ? AND strategy_instances.desired_running AND strategy_instances.delete_at IS NULL", r.cfg.InstanceID, r.cfg.Revision).
		Where("strategy_instances.account_id = ? AND strategy_instances.market_data_source = ? AND strategy_instances.template = ? AND strategy_instances.implementation = ?", AccountID(profile.UserId, profile.FundId), native.Source, domain.StrategyTemplateMarketMaker, strategyregistry.ImplementationMaker).
		Where("template.strategy_type = strategy_instances.template AND template.implementation = strategy_instances.implementation AND instrument.symbol = ? AND instrument.status = ?", profile.Symbol, domain.InstrumentStatusTrading).
		Count(&active).Error
	if err != nil {
		return err
	}
	if active != 1 {
		return native.ErrInvalid
	}
	return nil
}

func (r *Runtime) checkDirectQuoteBudget(ctx context.Context, cycle *quoteCycle, request gateway.QuoteRequest) error {
	budget, err := r.store.Budget(ctx, AccountID(request.Scope.UserID, request.Scope.FundID))
	if err != nil {
		return err
	}
	if !budget.Enabled || cycle.snapshot.GetMarket() == nil || budget.UserId != request.Scope.UserID || budget.FundId != request.Scope.FundID || budget.Symbol != request.Scope.Symbol ||
		cycle.snapshot.GetScope().GetUserId() != request.Scope.UserID || cycle.snapshot.GetScope().GetFundId() != request.Scope.FundID || cycle.snapshot.GetScope().GetSymbol() != request.Scope.Symbol {
		return ErrRiskLimit
	}
	gross, err := snapshotGross(cycle.snapshot, cycle.authority.Book)
	if err != nil {
		return err
	}
	added := request.Price.Mul(request.Quantity)
	for _, sent := range cycle.admitted {
		price, e1 := native.Value(sent.Price, false, false)
		quantity, e2 := native.Value(sent.Quantity, false, false)
		if e1 != nil || e2 != nil {
			return ErrInvalidSubmit
		}
		added = added.Add(price.Mul(quantity))
	}
	maximum, err := domain.ParseLedgerDecimal(budget.MaximumNotional, false, false)
	if err != nil || gross.Add(added).GreaterThan(maximum) {
		return ErrRiskLimit
	}
	live := proto.CloneOf(budget)
	live.AvailableQuote = cycle.snapshot.Available
	live.WorstFeeRate = cycle.snapshot.GetMarket().GetWorstFeeRate()
	return checkLiveBudget(live, SubmitRequest{
		Price:    request.Price,
		Quantity: request.Quantity,
		Leverage: request.Leverage,
	}, added)
}
