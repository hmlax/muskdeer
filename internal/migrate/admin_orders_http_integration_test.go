//go:build integration

package migrate

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminOrderHTTPReadsFundedUncertaintyAndHistoricalSourceWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "orders-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	other, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	core, intents := executionFixture(t, db)
	live, unsent, unknown := adminOrderFacts(ctx, t, db, core, intents, submit)
	if _, err := core.Fill(ctx, domain.CoreFillRequest{
		AccountID:           submit.Input.AccountID,
		ClientOrderID:       live.ClientOrderID,
		TradeID:             "admin-order-http-partial-fill",
		CounterpartySubject: "external-liquidity",
		Price:               decimal.NewFromInt(100),
		Quantity:            decimal.NewFromInt(1),
	}); err != nil {
		t.Fatal(err)
	}
	publishOrderHTTPSource(ctx, t, db, actor, submit)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	assertNativeOrderPeer(ctx, t, ca, discovery, viewer.Token, before.Id, live.ID)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "orders-viewer")
	path := "/api/v1/strategies/" + strconv.FormatInt(before.Id, 10) + "/orders"
	assertNativeOrderPages(ctx, t, client, base, path, live, unsent, unknown)
	assertNativeOrderScope(ctx, t, client, base, path, other.StrategyID, live.ID)
	assertNativeOrderLogout(ctx, t, client, jar, base, path, live.ID)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) {
		t.Fatal("read-only order delivery changed requested source or runtime authority")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "220")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "789.79", "110.11")
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 3)
	assertGatewayOrderCount(ctx, t, db, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.CancellationRequest{}, 0)
}

func publishOrderHTTPSource(ctx context.Context, t *testing.T, db *gorm.DB, actor domain.AdminLogin, submit domain.GatewaySubmit) {
	t.Helper()
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	publisher, err := repository.NewAdminStrategyPublisher(db)
	if err != nil {
		t.Fatal(err)
	}
	request := publicationRequest(adminCreationRequest(t, submit, domain.StrategyTemplateMarketMaker, domain.MarketSourceBinance), before.Id, before.Revision, before.RequestedConfigVersion)
	if _, err := publisher.Publish(ctx, actor.Token, actor.CSRF, request); err != nil {
		t.Fatal(err)
	}
	after := publicationInstance(ctx, t, db, before.Id)
	if after.RequestedConfigVersion != 2 || after.MarketDataSource != string(domain.MarketSourceOKX) {
		t.Fatal("order HTTP source fixture did not publish a different requested source")
	}
}

func assertNativeOrderPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, strategyID, orderID int64) {
	t.Helper()
	request := &strategyv1.GetStrategyOrderRequest{
		Session:    &strategyv1.AdminSession{Token: token},
		StrategyId: strategyID,
		OrderId:    orderID,
	}
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	reply, err := strategyv1.NewStrategyOrderQueryClient(api).GetStrategyOrder(ctx, request)
	if err != nil || reply.Order.State != string(domain.OrderStateLive) || reply.Order.Core == nil || reply.Order.Core.State != string(domain.OrderStatePartiallyFilled) || reply.Order.ReservedNotional != "200" || reply.Order.Core.FrozenAmount != "100.1" {
		t.Fatal("actual API-peer order RPC lost funded independent observations", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyOrderQueryClient(other).GetStrategyOrder(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid other-service certificate read actual orders", err)
	}
	if _, err := strategyv1.NewStrategyControlClient(api).ControlStrategy(ctx, &strategyv1.ControlStrategyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only strategy registered trading controls", err)
	}
}

func assertNativeOrderPages(ctx context.Context, t *testing.T, client *http.Client, base, path string, live, unsent, unknown domain.IntentSnapshot) {
	t.Helper()
	previous := int64(0)
	for index, intent := range []domain.IntentSnapshot{live, unsent, unknown} {
		var page strategyv1.ListStrategyOrdersReply
		readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&after_id="+strconv.FormatInt(previous, 10), &page)
		if len(page.Orders) != 1 || page.Orders[0].Id != intent.ID || page.Orders[0].ConfigVersion != 1 || page.Orders[0].MarketDataSource != string(domain.MarketSourceBinance) || (index < 2 && page.NextAfterId != intent.ID) || (index == 2 && page.NextAfterId != 0) {
			t.Fatal("actual order HTTP crossed cursor or used current requested source")
		}
		previous = page.NextAfterId
	}
	var partial, missing, uncertain strategyv1.GetStrategyOrderReply
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(live.ID, 10), &partial)
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(unsent.ID, 10), &missing)
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(unknown.ID, 10), &uncertain)
	if partial.Order.State != string(domain.OrderStateLive) || partial.Order.FilledQuantity != "0" || partial.Order.Core.FilledQuantity != "1" || partial.Order.ConfirmedCoreRevision >= partial.Order.Core.Revision || missing.Order.Core != nil || missing.Order.ReservedNotional != "10" || uncertain.Order.State != string(domain.OrderStateUnknown) || uncertain.Order.ConfirmedCoreId != 0 || uncertain.Order.Core == nil || uncertain.Order.Core.FrozenAmount != "10.01" {
		t.Fatal("actual HTTP silently reconciled OMS or hid committed uncertainty")
	}
	var filtered strategyv1.ListStrategyOrdersReply
	readNativeStrategyProto(ctx, t, client, base, path+"?state="+string(domain.OrderStateUnknown)+"&open_only=true", &filtered)
	if len(filtered.Orders) != 1 || filtered.Orders[0].Id != unknown.ID || filtered.NextAfterId != 0 {
		t.Fatal("actual HTTP ignored unknown/open obligation filters")
	}
}

func assertNativeOrderScope(ctx context.Context, t *testing.T, client *http.Client, base, path string, otherID, orderID int64) {
	t.Helper()
	other := "/api/v1/strategies/" + strconv.FormatInt(otherID, 10) + "/orders"
	var page strategyv1.ListStrategyOrdersReply
	readNativeStrategyProto(ctx, t, client, base, other, &page)
	if len(page.Orders) != 0 || page.NextAfterId != 0 {
		t.Fatal("empty sibling strategy exposed another strategy's orders")
	}
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{
			method: "GET",
			path:   other + "/" + strconv.FormatInt(orderID, 10),
			status: 404,
		},
		{
			method: "GET",
			path:   path + "?state=FILLED&open_only=true",
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
			t.Fatal("order HTTP failed scope, input or read-only boundary", err, reply.StatusCode)
		}
	}
}

func assertNativeOrderLogout(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, path string, id int64) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := jar.Cookies(parsed)
	var csrf string
	for _, cookie := range cookies {
		if cookie.Name == "muskdeer_csrf" {
			csrf = cookie.Value
		}
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual order viewer logout failed", err)
	}
	for _, route := range []string{path, path + "/" + strconv.FormatInt(id, 10)} {
		jar.SetCookies(parsed, cookies)
		reply := nativeAdminRequest(ctx, t, client, base, "GET", route, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
			t.Fatal("restored revoked cookie read actual orders", err, reply.StatusCode)
		}
	}
}
