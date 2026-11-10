//go:build integration

package console

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func TestCreateAccountRequiresAuthorizedUserAndVerifiedFundThenRegistersOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	db := testDatabase(ctx, t)
	product := &model.Instrument{
		Symbol:          "AVAXUSDT",
		BaseAsset:       "AVAX",
		QuoteAsset:      "USDT",
		PriceTick:       "0.01",
		QuantityStep:    "0.001",
		MinNotional:     "1",
		Status:          domain.InstrumentStatusTrading,
		ExecutionVenue:  native.Venue,
		MetadataVersion: 1,
	}
	if err := db.WithContext(ctx).Model(&model.Instrument{}).Create(product).Error; err != nil {
		t.Fatal(err)
	}
	sponsor := &model.TradingAccountPolicy{
		AccountId:            native.ScopeKey(70000, 70000),
		UserId:               70000,
		FundId:               70000,
		Symbol:               product.Symbol,
		MarketDataSource:     native.Source,
		PriceTick:            product.PriceTick,
		QuantityStep:         product.QuantityStep,
		MaximumNotional:      "1000",
		MaximumOrderNotional: "100",
		Leverage:             10,
		Enabled:              true,
		ObservedNotional:     "0",
		ReservedNotional:     "0",
		AvailableQuote:       "0",
		WorstFeeRate:         "0",
	}
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Create(sponsor).Error; err != nil {
		t.Fatal(err)
	}
	var observations atomic.Int64
	wrongFund := true
	server := &Server{
		db:       db,
		controls: true,
		accountInspector: func(_ context.Context, scope *common.VenueScope) (*common.VenueSnapshot, error) {
			observations.Add(1)
			observedScope := proto.CloneOf(scope)
			if wrongFund {
				observedScope.FundId++
			}
			return &common.VenueSnapshot{
				Scope:        observedScope,
				ValidUntilMs: time.Now().Add(time.Second).UnixMilli(),
				Market: &common.VenueMarket{
					PriceTick:       "0.01",
					QuantityStep:    "0.001",
					MaximumLeverage: 10,
				},
			}, nil
		},
	}
	operator := operatorContext(ctx, rpcsecurity.ServiceOperator)
	account := native.ScopeKey(70000, 70001)
	request := &pb.CreateAccountRequest{
		Actor: &pb.Actor{
			Subject:           "user:2",
			TenantId:          7,
			RequestId:         "550e8400-e29b-41d4-a716-446655440000",
			AllowedAccountIds: []string{sponsor.AccountId},
		},
		Data: &pb.AccountDraft{
			UserId:               "70000",
			FundId:               "70001",
			InstrumentId:         strconv.FormatInt(product.Id, 10),
			MarketDataSource:     native.Source,
			MaximumNotional:      "1000",
			MaximumOrderNotional: "100",
			Leverage:             10,
		},
	}
	if _, err := server.CreateAccount(operator, request); status.Code(err) != codes.PermissionDenied || observations.Load() != 0 {
		t.Fatalf("unreserved fund reached SDK: %v", err)
	}
	request.Actor.AllowedAccountIds = []string{account}
	if _, err := server.CreateAccount(operator, request); status.Code(err) != codes.PermissionDenied || observations.Load() != 0 {
		t.Fatalf("unsponsored user reached SDK: %v", err)
	}
	request.Actor.AllowedAccountIds = []string{sponsor.AccountId, account}
	if _, err := server.CreateAccount(operator, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign fund observation accepted: %v", err)
	}
	wrongFund = false
	var group sync.WaitGroup
	results := make(chan *pb.Account, 2)
	errors := make(chan error, 2)
	for range 2 {
		group.Go(func() { row, err := server.CreateAccount(operator, request); results <- row; errors <- err })
	}
	group.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var id int64
	for row := range results {
		if row == nil || row.Id <= 0 || row.AccountId != account || id != 0 && id != row.Id {
			t.Fatalf("concurrent registration result: %v", row)
		}
		id = row.Id
	}
	request.Data.MaximumNotional = "2000"
	if _, err := server.CreateAccount(operator, request); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting create overwrote account: %v", err)
	}
	var saved model.TradingAccountPolicy
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("account_id = ?", account).Take(&saved).Error; err != nil {
		t.Fatal(err)
	}
	maximum, err := domain.ParseLedgerDecimal(saved.MaximumNotional, false, false)
	if err != nil || maximum.String() != "1000" {
		t.Fatalf("registered policy changed: %v %v", maximum, err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.TradingAccountPolicy{}).Where("user_id = ?", 70000).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("registration rows=%d error=%v", count, err)
	}
}
