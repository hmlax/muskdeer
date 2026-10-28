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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminCatalogHTTPReadsRealManagementProcessAndRejectsRevokedBearer(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	account := &modelv1.SimulatorAccount{
		AccountId:    "catalog-native-account",
		SubjectId:    "catalog-native-subject",
		Enabled:      true,
		QuoteFeeRate: "0.123456789012345678",
		Revision:     1,
	}
	if err := db.WithContext(ctx).Model(&modelv1.SimulatorAccount{}).Create(account).Error; err != nil {
		t.Fatal(err)
	}
	instrument := identityInstrument()
	if err := db.WithContext(ctx).Model(&modelv1.Instrument{}).Create(instrument).Error; err != nil {
		t.Fatal(err)
	}
	catalogFeedFixtures(ctx, t, db, instrument.Id)
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
	assertNativeCatalogRPC(ctx, t, ca, discovery, actor.Token, instrument.Id)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{
		MaxIdleConns:    2,
		MaxConnsPerHost: 2,
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Jar:       jar,
		Transport: transport,
		Timeout:   3 * time.Second,
	}
	login := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", `{"username":"control-operator","password":"`+adminFixturePassword+`"}`, base, "")
	if err := login.Body.Close(); err != nil || login.StatusCode != 200 {
		t.Fatal("catalog viewer could not log in through actual HTTP")
	}
	assertNativeCatalogHTTP(ctx, t, client, base, account, instrument)
	assertNativeCatalogLogout(ctx, t, client, base, jar, instrument.Id)
	assertGatewayOrderCount(ctx, t, db, 0)
	for _, entity := range []any{&modelv1.StrategyInstance{}, &modelv1.StrategyLease{}, &modelv1.AccountBudget{}, &modelv1.MarketPublisherLease{}, &modelv1.SimulatorBalance{}} {
		assertPortfolioRowCount(ctx, t, db, entity, 0)
	}
}

func assertNativeCatalogRPC(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, instrumentID int64) {
	t.Helper()
	client := strategyv1.NewStrategyCatalogClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI))
	session := &strategyv1.AdminSession{Token: token}
	accounts, err := client.ListAccounts(ctx, &strategyv1.ListAccountsRequest{
		Session: session,
		Limit:   1,
	})
	if err != nil || len(accounts.Accounts) != 1 || accounts.Accounts[0].QuoteFeeRate != "0.123456789012345678" {
		t.Fatal("actual catalog RPC lost account precision", err)
	}
	feeds, err := client.ListMarketFeeds(ctx, &strategyv1.ListMarketFeedsRequest{
		Session:      session,
		InstrumentId: instrumentID,
		Mode:         string(domain.MarketModeReplay),
		Limit:        2,
	})
	if err != nil || len(feeds.Feeds) != 2 || feeds.Feeds[0].Source != string(domain.MarketSourceBinance) || feeds.Feeds[1].Source != string(domain.MarketSourceOKX) {
		t.Fatal("actual catalog RPC mixed provider bindings", err)
	}
	other := strategyv1.NewStrategyCatalogClient(adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio))
	if _, err := other.ListInstruments(ctx, &strategyv1.ListInstrumentsRequest{
		Session: session,
		Limit:   1,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("non-API service certificate reached actual catalog", err)
	}
}

func assertNativeCatalogHTTP(ctx context.Context, t *testing.T, client *http.Client, base string, account *modelv1.SimulatorAccount, instrument *modelv1.Instrument) {
	t.Helper()
	var accounts strategyv1.ListAccountsReply
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/accounts?limit=1", &accounts)
	if len(accounts.Accounts) != 1 || accounts.Accounts[0].Id != account.Id || accounts.Accounts[0].QuoteFeeRate != account.QuoteFeeRate {
		t.Fatal("actual catalog HTTP fabricated or rounded account data")
	}
	var instruments strategyv1.ListInstrumentsReply
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/instruments?limit=1", &instruments)
	if len(instruments.Instruments) != 1 || instruments.Instruments[0].Id != instrument.Id || instruments.Instruments[0].MinNotional != instrument.MinNotional {
		t.Fatal("actual catalog HTTP lost execution identity or exact minimum")
	}
	path := "/api/v1/instruments/" + strconv.FormatInt(instrument.Id, 10) + "/market-feeds"
	var feeds strategyv1.ListMarketFeedsReply
	readNativeStrategyProto(ctx, t, client, base, path+"?mode=REPLAY&limit=1", &feeds)
	if len(feeds.Feeds) != 1 || feeds.Feeds[0].Source != string(domain.MarketSourceBinance) || feeds.NextAfterId != feeds.Feeds[0].Id {
		t.Fatal("actual catalog HTTP lost bounded first source")
	}
	var next strategyv1.ListMarketFeedsReply
	readNativeStrategyProto(ctx, t, client, base, path+"?mode=REPLAY&limit=1&after_id="+strconv.FormatInt(feeds.NextAfterId, 10), &next)
	if len(next.Feeds) != 1 || next.Feeds[0].Source != string(domain.MarketSourceOKX) || next.Feeds[0].Policy == nil || next.NextAfterId != 0 {
		t.Fatal("actual catalog HTTP substituted the second source or omitted policy")
	}
	var live strategyv1.ListMarketFeedsReply
	readNativeStrategyProto(ctx, t, client, base, path+"?mode=LIVE", &live)
	if len(live.Feeds) != 0 {
		t.Fatal("actual catalog HTTP promoted replay to live")
	}
}

func assertNativeCatalogLogout(ctx context.Context, t *testing.T, client *http.Client, base string, jar *cookiejar.Jar, instrumentID int64) {
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
		t.Fatal("actual catalog session logout failed")
	}
	for _, path := range []string{"/api/v1/accounts", "/api/v1/instruments", "/api/v1/instruments/" + strconv.FormatInt(instrumentID, 10) + "/market-feeds?mode=REPLAY"} {
		jar.SetCookies(parsed, cookies)
		reply := nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
			t.Fatal("actual catalog accepted the restored revoked bearer", path)
		}
	}
}
