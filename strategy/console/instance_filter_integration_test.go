//go:build integration

package console

import (
	"context"
	"strconv"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	strategy "github.com/hmlax/muskdeer/deer/pb/strategy/model/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/strategyregistry"
)

func TestInstanceStatusFilterKeepsScopeTotalsAndPagination(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db := testDatabase(ctx, t)
	product := &model.Instrument{
		Symbol:          "FILTERUSDT",
		BaseAsset:       "FILTER",
		QuoteAsset:      "USDT",
		ExecutionVenue:  native.Venue,
		PriceTick:       "0.01",
		QuantityStep:    "1",
		MinNotional:     "1",
		Status:          domain.InstrumentStatusTrading,
		MetadataVersion: 1,
	}
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Create(product).Error; err != nil {
		t.Fatal(err)
	}
	accounts := []string{native.ScopeKey(81001, 81001), native.ScopeKey(81002, 81002), native.ScopeKey(81003, 81003)}
	for index, account := range accounts {
		row := &model.TradingAccountPolicy{
			AccountId:            account,
			UserId:               int64(81001 + index),
			FundId:               int64(81001 + index),
			Symbol:               product.Symbol,
			MaximumNotional:      "100",
			MaximumOrderNotional: "100",
			ObservedNotional:     "0",
			ReservedNotional:     "0",
			AvailableQuote:       "0",
			WorstFeeRate:         "0",
			MarketDataSource:     native.Source,
			PriceTick:            product.PriceTick,
			QuantityStep:         product.QuantityStep,
			Leverage:             10,
		}
		if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index := range 6 {
		row, err := instanceconfig.ManageConfigured(ctx, db, instanceconfig.ManagementWrite{
			Operation: instanceconfig.ManagementCreate,
			Subject:   "status-filter-test",
			RequestID: "instance-status-filter-create-" + strconv.Itoa(index),
			Input: &common.StrategyInstanceInput{
				InstanceKey:      "status-filter-" + strconv.Itoa(index),
				TemplateId:       strategyregistry.MakerTemplateID,
				TemplateRevision: 1,
				AccountId:        accounts[index%3],
				InstrumentId:     product.Id,
				MarketDataSource: native.Source,
				Parameters: &common.StrategyInstanceParameters{
					SchemaVersion: 1,
					Execution: &common.StrategyExecutionParameters{
						Leverage:           10,
						PollIntervalMs:     200,
						OperationTimeoutMs: 2000,
						LeaseTtlMs:         10000,
					},
					MarketMaker: &common.DepthMakerParameters{
						Depth:               20,
						QuantityFraction:    "0.1",
						ReplacementFraction: "0.1",
						BridgeSlots:         1,
					},
				},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if index < 3 {
			if _, err := instanceconfig.ManageConfigured(ctx, db, instanceconfig.ManagementWrite{
				Operation:              instanceconfig.ManagementStart,
				Subject:                "status-filter-test",
				RequestID:              "instance-status-filter-start-" + strconv.Itoa(index),
				ID:                     row.Id,
				ExpectedRevision:       row.Revision,
				ExpectedControlVersion: row.RequestedConfigVersion,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	server := &Server{db: db}
	actor := &pb.Actor{
		TenantId:          7,
		AllowedAccountIds: accounts[:2],
	}
	for _, test := range []struct {
		name    string
		filter  *bool
		account string
		search  string
		total   int64
	}{
		{name: "all", total: 4},
		{name: "started", filter: proto.Bool(true), total: 2},
		{name: "stopped", filter: proto.Bool(false), total: 2},
		{name: "stopped account", filter: proto.Bool(false), account: accounts[0], total: 1},
		{name: "stopped search", filter: proto.Bool(false), search: "status-filter-3", total: 1},
		{name: "foreign account", filter: proto.Bool(false), account: accounts[2], total: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &pb.InstanceListRequest{
				Actor:          actor,
				DesiredRunning: test.filter,
				AccountId:      test.account,
				Search:         test.search,
				StrategyType:   string(domain.StrategyTemplateMarketMaker),
			}
			var total int64
			if err := server.instanceListQuery(ctx, request).Count(&total).Error; err != nil {
				t.Fatal(err)
			}
			if total != test.total {
				t.Fatalf("filtered total=%d, want %d", total, test.total)
			}
			seen := make(map[int64]bool)
			for offset := 0; offset < int(total); offset++ {
				var page []*strategy.StrategyInstance
				if err := server.instanceListQuery(ctx, request).Order("id").Offset(offset).Limit(1).Find(&page).Error; err != nil {
					t.Fatal(err)
				}
				if len(page) != 1 || seen[page[0].Id] || page[0].AccountId == accounts[2] || test.filter != nil && page[0].DesiredRunning != *test.filter {
					t.Fatalf("invalid scoped page: %v", page)
				}
				seen[page[0].Id] = true
			}
		})
	}
}
