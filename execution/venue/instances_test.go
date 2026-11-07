package venue

import (
	"context"
	"sync/atomic"
	"testing"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/venueconfig"
	"github.com/shopspring/decimal"
)

func TestInstanceDispatchCannotUseLegacyIDAnotherRevisionOrAlgorithm(t *testing.T) {
	maker := &Runtime{cfg: &venueconfig.RuntimeConfig{
		InstanceID: 1,
		Revision:   3,
		Template:   domain.StrategyTemplateMarketMaker,
	}}
	wash := &Runtime{cfg: &venueconfig.RuntimeConfig{
		InstanceID: 2,
		Revision:   1,
		Template:   domain.StrategyTemplateWashTrade,
	}}
	parent := &Instances{children: map[int64]*Runtime{
		1: maker,
		2: wash,
	}}
	for _, plan := range []*execution.QuotePlan{
		{},
		{
			StrategyInstanceId: 1,
			StrategyRevision:   2,
		},
		{
			StrategyInstanceId: 2,
			StrategyRevision:   1,
		},
	} {
		if _, err := parent.Reconcile(context.Background(), plan); err == nil {
			t.Fatal("invalid instance route accepted")
		}
	}
	if _, err := parent.selected(1, 3, false); err != nil {
		t.Fatal(err)
	}
	if _, err := parent.selected(2, 1, true); err != nil {
		t.Fatal(err)
	}
}
func TestIndependentFundInstancesDoNotRetainOrCancelEachOthersOrders(t *testing.T) {
	profile := &common.VenueProfilePolicy{
		Symbol:               "BTCUSDT",
		StrategyInstanceId:   1,
		MaximumOrderNotional: "100",
	}
	snapshot := &common.VenueSnapshot{Orders: []*common.VenueOrder{
		{
			OrderId:          "10",
			ClientId:         "deer_BTCUSDT_s2_foreign",
			Side:             string(domain.SideBuy),
			Price:            "99",
			Quantity:         "1",
			ExecutedQuantity: "0",
			Status:           native.StatusOpen,
		},
	}}
	retained, err := retainQuoteOrders(profile, snapshot, map[string]*SubmitRequest{
		"BUY/99": {
			Side:     domain.SideBuy,
			Price:    decimal.NewFromInt(99),
			Quantity: decimal.NewFromInt(1),
		},
	}, true, decimal.NewFromInt(1))
	if err != nil {
		t.Fatal(err)
	}
	if retained.orders["10"] || retained.targets["BUY/99"] {
		t.Fatal("foreign instance counted as own target")
	}
	// Shared fund admission still excludes overlap; plans remain instance-specific.
	gate := make(chan struct{}, 1)
	client := &atomic.Pointer[gatewaySession]{}
	client.Store(&gatewaySession{})
	first := &Runtime{
		gates:   map[string]chan struct{}{AccountID(1, 1): gate},
		client:  client,
		closing: &atomic.Bool{},
	}
	second := &Runtime{
		gates:   first.gates,
		client:  client,
		closing: first.closing,
	}
	leave, err := first.enter(&common.VenueScope{
		UserId: 1,
		FundId: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.enter(&common.VenueScope{
		UserId: 1,
		FundId: 1,
	}); err == nil {
		t.Fatal("independent instance bypassed shared fund admission")
	}
	leave()
}

func TestInstanceCancellationPreservesOtherActorsAndCompletesLegacyCutover(t *testing.T) {
	profile := &common.VenueProfilePolicy{
		Symbol:             "BTCUSDT",
		StrategyInstanceId: 1,
	}
	if ownsCancellation(profile, "deer_BTCUSDT_s2_foreign", false) || ownsCancellation(profile, "deer_BTCUSDT_s2_foreign", true) {
		t.Fatal("foreign strategy order can be cancelled")
	}
	if !ownsCancellation(profile, "deer_BTCUSDT_s1_own", true) {
		t.Fatal("current instance cannot maintain its quotes")
	}
	if !ownsCancellation(profile, "mm_BTCUSDT_legacy", false) || ownsCancellation(profile, "mm_BTCUSDT_legacy", true) {
		t.Fatal("legacy cutover permission changed")
	}
}
