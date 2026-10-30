//go:build integration

package migrate

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminTradeHTTPReadsAuthoritativeFillsAndIncompleteCollectionWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "trades-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	buy, sell := portfolioTrades(ctx, t, core, intents, submit)
	publishOrderHTTPSource(ctx, t, db, actor, submit)
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell)); err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatal("HTTP fixture did not retain incomplete collected facts", err)
	}
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	ca := testcert.New(t)
	naming := businessNaming()
	discovery, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := discovery.Close(); err != nil {
			t.Error(err)
		}
	})
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	assertNativeTradePeer(ctx, t, ca, discovery, viewer.Token, before.Id, buy.AggregateId)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "trades-viewer")
	path := "/api/v1/strategies/" + strconv.FormatInt(before.Id, 10) + "/trades"
	assertNativeTradePages(ctx, t, client, base, path, buy.AggregateId, sell.AggregateId, buy.Fill.IntentId)
	assertNativeTradeScope(ctx, t, client, base, path, other.StrategyID, buy.AggregateId, buy.Fill.IntentId)
	assertNativeOrderLogout(ctx, t, client, jar, base, path, buy.AggregateId)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("trade delivery mutated runtime or repaired incomplete positions")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 2)
	assertGatewayOrderCount(ctx, t, db, 2)
}

func assertNativeTradePeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, strategyID, tradeID int64) {
	t.Helper()
	request := &strategyv1.GetStrategyTradeRequest{
		Session:    &strategyv1.AdminSession{Token: token},
		StrategyId: strategyID,
		TradeId:    tradeID,
	}
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	reply, err := strategyv1.NewStrategyTradeQueryClient(api).GetStrategyTrade(ctx, request)
	if err != nil || reply.Trade.Id != tradeID || reply.Trade.FeeAmount != "0.099" || reply.Trade.OrderId == nil || reply.Trade.PortfolioFact != nil {
		t.Fatal("actual API-peer trade RPC lost authority or collection absence", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyTradeQueryClient(other).GetStrategyTrade(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API service certificate read trades", err)
	}
	if _, err := strategyv1.NewStrategyControlClient(api).ControlStrategy(ctx, &strategyv1.ControlStrategyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only strategy registered trading controls", err)
	}
}

func assertNativeTradePages(ctx context.Context, t *testing.T, client *http.Client, base, path string, buyID, sellID, orderID int64) {
	t.Helper()
	previous := int64(0)
	for index, id := range []int64{buyID, sellID} {
		var page strategyv1.ListStrategyTradesReply
		readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&after_id="+strconv.FormatInt(previous, 10), &page)
		if len(page.Trades) != 1 || page.Trades[0].Id != id || page.Trades[0].ConfigVersion != 1 || page.Trades[0].MarketDataSource != string(domain.MarketSourceBinance) || index == 0 && page.NextAfterId != id || index == 1 && page.NextAfterId != 0 {
			t.Fatal("actual trade HTTP lost cursor, source history or exact scope")
		}
		previous = page.NextAfterId
	}
	var buy, sell strategyv1.GetStrategyTradeReply
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(buyID, 10), &buy)
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(sellID, 10), &sell)
	if buy.Trade.PortfolioFact != nil || sell.Trade.PortfolioFact == nil || buy.Trade.FeeAmount != "0.099" || sell.Trade.FeeAmount != "0.101" || buy.Trade.GetOrderId() != orderID {
		t.Fatal("actual HTTP hid an authoritative fill or invented collection")
	}
	var filtered strategyv1.ListStrategyTradesReply
	readNativeStrategyProto(ctx, t, client, base, path+"?order_id="+strconv.FormatInt(orderID, 10)+"&side=BUY", &filtered)
	if len(filtered.Trades) != 1 || filtered.Trades[0].Id != buyID || filtered.NextAfterId != 0 {
		t.Fatal("actual HTTP lost original OMS and side filter")
	}
}

func assertNativeTradeScope(ctx context.Context, t *testing.T, client *http.Client, base, path string, otherID, tradeID, orderID int64) {
	t.Helper()
	other := "/api/v1/strategies/" + strconv.FormatInt(otherID, 10) + "/trades"
	var empty strategyv1.ListStrategyTradesReply
	readNativeStrategyProto(ctx, t, client, base, other, &empty)
	if len(empty.Trades) != 0 || empty.NextAfterId != 0 {
		t.Fatal("sibling strategy exposed authoritative fills")
	}
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{
			method: "GET",
			path:   other + "/" + strconv.FormatInt(tradeID, 10),
			status: 404,
		},
		{
			method: "GET",
			path:   other + "?order_id=" + strconv.FormatInt(orderID, 10),
			status: 404,
		},
		{
			method: "GET",
			path:   path + "?side=buy",
			status: 400,
		},
		{
			method: "POST",
			path:   path,
			status: 405,
		},
	} {
		reply := nativeAdminRequest(ctx, t, client, base, test.method, test.path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != test.status {
			t.Fatal("actual trade HTTP failed scope or read-only contract", err, reply.StatusCode)
		}
	}
}
