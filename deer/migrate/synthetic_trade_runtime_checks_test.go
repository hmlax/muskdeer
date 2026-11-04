//go:build integration

package migrate

import (
	"context"
	"testing"

	pb "gitlab.com/himalaya/lukla-go/pb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
	executionhimalayav1 "github.com/hmlax/muskdeer/deer/pb/execution/himalaya/v1"
	executionmodelv1 "github.com/hmlax/muskdeer/deer/pb/execution/model/v1"
	markethimalayav1 "github.com/hmlax/muskdeer/deer/pb/market/himalaya/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/repository"
)

type syntheticTradeOutageCheck struct {
	market  *nativeMarketFixture
	sdk     *nativeSDKFixture
	db      *gorm.DB
	factory *rpcclient.Factory
	cfg     *commonv1.HimalayaPolicy
	owner   string
}

func assertSyntheticTradeOutage(ctx context.Context, t *testing.T, check syntheticTradeOutageCheck) {
	t.Helper()
	nativeWait(ctx, t, func() bool {
		_, err := repository.NativeExecutionInstance(ctx, check.db, native.SyntheticExecutionKey(check.cfg.SyntheticTape.RunId, "BTCUSDT"))
		return check.market.tradeRequests.Load() >= 2 && err == nil
	})
	var count int64
	if err := check.db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Count(&count).Error; err != nil || count != 0 || check.sdk.syntheticCount() != 0 {
		t.Fatalf("trade outage slots=%d SDK calls=%d err=%v, want 0/0", count, check.sdk.syntheticCount(), err)
	}
	connection, err := check.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceMarket})
	if err != nil {
		t.Fatal(err)
	}
	book, err := markethimalayav1.NewMarketClient(connection).GetBook(ctx, &markethimalayav1.BookRequest{Symbol: "BTCUSDT"})
	if err != nil || len(book.GetBids()) == 0 || len(book.GetAsks()) == 0 {
		t.Fatalf("trade outage disturbed independent depth source: %v", err)
	}
	connection, err = check.factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceExecution})
	if err != nil {
		t.Fatal(err)
	}
	legacy := &commonv1.SyntheticTapePlan{
		RunId:    check.cfg.SyntheticTape.RunId,
		Symbol:   "BTCUSDT",
		Side:     string(domain.SideBuy),
		Price:    "100.1",
		Quantity: "0.1",
		Book:     book,
		Strategy: &commonv1.HimalayaAuthority{
			AccountId: native.SyntheticStrategyKey(check.cfg.SyntheticTape.RunId, "BTCUSDT"),
			OwnerId:   check.owner,
			Epoch:     1,
		},
	}
	if _, err := executionhimalayav1.NewExecutionClient(connection).PublishSynthetic(ctx, legacy); status.Code(err) != codes.InvalidArgument || check.sdk.syntheticCount() != 0 {
		t.Fatalf("legacy wire in aggregate mode: err=%v effects=%d, want InvalidArgument/0", err, check.sdk.syntheticCount())
	}
}

func assertSyntheticTradeReceipts(ctx context.Context, t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []*executionmodelv1.SyntheticTapeIntent
	if err := db.WithContext(ctx).Model(&executionmodelv1.SyntheticTapeIntent{}).Order("sequence").Find(&rows).Error; err != nil || len(rows) != 2 {
		t.Fatalf("aggregate runtime audit rows=%d err=%v, want 2", len(rows), err)
	}
	var previous int64
	for _, row := range rows {
		id, err := native.TradeSequence(row.Plan.SourceTrade)
		if err != nil || id <= previous || !proto.Equal(row.Plan.SourceTrade, row.Result.SourceTrade) || row.Plan.Side != string(domain.SideSell) || row.Plan.Price != "100.4" || row.Plan.Quantity != "0.07" {
			t.Fatalf("aggregate runtime lost authoritative typed source: %v, %v", row, err)
		}
		previous = id
	}
}

func assertSyntheticSDKDirections(t *testing.T, mode configv1.SyntheticTapeMode, inputs []*pb.WashTradeRequest) {
	t.Helper()
	if mode == configv1.SyntheticTapeMode_SYNTHETIC_TAPE_MODE_AGG_TRADE {
		for _, input := range inputs {
			if input.Side != int32(pb.Side_SIDE_SELL) || input.Price != "100.4" || input.Quantity != "0.07" {
				t.Fatalf("aggregate SDK payload=%v, want source SELL/100.4/0.07", input)
			}
		}
	} else if inputs[0].Side != int32(pb.Side_SIDE_BUY) || inputs[1].Side != int32(pb.Side_SIDE_SELL) {
		t.Fatal("synthetic directions did not alternate")
	}
}
