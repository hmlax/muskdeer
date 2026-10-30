//go:build integration

package migrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
)

func TestAdminMarketHTTPReadsActualHealthWithoutPublishingOrGrantingRisk(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	actor := adminControlSessionFixtureForRole(ctx, t, db, domain.AdminRoleViewer)
	instrumentID := marketInstrumentFixture(ctx, t, db)
	feeds := catalogFeedFixtures(ctx, t, db, instrumentID)
	ca, discovery, naming := cancellationDiscovery(ctx, t)
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, _ := nativeRiskHTTPClient(t)
	csrf := nativeMarketLogin(ctx, t, client, base)
	assertNativeMarketPeer(ctx, t, adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio), actor.Token)
	if empty := nativeMarketRead(ctx, t, client, base, feeds[1].ID); empty.Snapshot != nil || empty.Feed.Source != string(domain.MarketSourceOKX) {
		t.Fatal("actual unpublished OKX view borrowed a quote")
	}
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := ownerid.New(ctx, "admin-market-http")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := leases.Acquire(ctx, domain.LeaseKey{
		Kind:         domain.LeaseKindMarket,
		MarketFeedID: feeds[0].ID,
	}, owner, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	snapshots, err := repository.NewMarketSnapshots(db)
	if err != nil {
		t.Fatal(err)
	}
	reading := marketReadingFixture(feeds[0], 9007199254740993, "100")
	reading.Bid, reading.Ask, reading.Mid = decimal.RequireFromString("99.999999999999999999"), decimal.NewFromInt(100), decimal.RequireFromString("99.9999999999999999995")
	publication, err := snapshots.Publish(ctx, authority, 0, reading)
	if err != nil {
		t.Fatal(err)
	}
	before := adminMarketStoredSnapshot(ctx, t, db, feeds[0].ID)
	value := nativeMarketRead(ctx, t, client, base, feeds[0].ID)
	if value.Snapshot == nil || !value.Snapshot.PublisherCurrent || value.Snapshot.Reading.Quality != string(domain.MarketQualityHealthy) || value.Snapshot.Reading.MidPrice != "99.9999999999999999995" || value.Snapshot.Reading.Sequence != 9007199254740993 {
		t.Fatal("actual HTTP lost healthy authority or exact prices/sequence")
	}
	assertNativeMarketPages(ctx, t, client, base, feeds)
	waitMarketDatabaseExpiry(ctx, t, db, feeds[0].ID, publication.Reading.ValidUntil)
	stale := nativeMarketRead(ctx, t, client, base, feeds[0].ID)
	if stale.Snapshot == nil || !stale.Snapshot.PublisherCurrent || stale.Snapshot.Reading.Quality != string(domain.MarketQualityStale) || stale.Snapshot.Reading.MidPrice != "0" || stale.Snapshot.PublishedQuality != string(domain.MarketQualityHealthy) {
		t.Fatal("actual HTTP retained stale prices or hid historical diagnostics")
	}
	if err := leases.Release(ctx, authority); err != nil {
		t.Fatal(err)
	}
	lost := nativeMarketRead(ctx, t, client, base, feeds[0].ID)
	if lost.Snapshot == nil || lost.Snapshot.PublisherCurrent || lost.Snapshot.Reading.Quality != string(domain.MarketQualityUnavailable) || lost.Snapshot.Reading.MidPrice != "0" {
		t.Fatal("actual HTTP trusted a released publisher")
	}
	assertNativeMarketLogout(ctx, t, client, base, csrf, feeds[0].ID)
	if !proto.Equal(before, adminMarketStoredSnapshot(ctx, t, db, feeds[0].ID)) {
		t.Fatal("market HTTP rewrote publication diagnostics")
	}
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketPublisherLease{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.MarketSnapshot{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.OrderIntent{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyInstance{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.RiskSnapshot{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 0)
}

func nativeMarketRead(ctx context.Context, t *testing.T, client *http.Client, base string, id int64) *strategyv1.MarketMonitor {
	t.Helper()
	reply := nativeAdminRequest(ctx, t, client, base, "GET", "/api/v1/markets/"+strconv.FormatInt(id, 10), "", "", "")
	body, readErr := io.ReadAll(io.LimitReader(reply.Body, 128*1024))
	closeErr := reply.Body.Close()
	var result strategyv1.GetMarketReply
	if reply.StatusCode != 200 || readErr != nil || closeErr != nil || reply.Header.Get("Cache-Control") != "no-store" || protojson.Unmarshal(body, &result) != nil || result.Market == nil {
		t.Fatal("actual market HTTP failed its typed no-store response")
	}
	var wire struct {
		Market struct {
			Feed struct {
				ID string `json:"id"`
			} `json:"feed"`
			Snapshot *struct {
				Epoch   string `json:"publisher_epoch"`
				Reading struct {
					Mid      string `json:"mid_price"`
					Sequence string `json:"sequence"`
				} `json:"reading"`
			} `json:"snapshot"`
		} `json:"market"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Market.Feed.ID != strconv.FormatInt(id, 10) {
		t.Fatal("actual market response did not retain string identities", err)
	}
	if result.Market.Snapshot != nil && (wire.Market.Snapshot == nil || wire.Market.Snapshot.Epoch != strconv.FormatInt(result.Market.Snapshot.PublisherEpoch, 10) || wire.Market.Snapshot.Reading.Sequence != strconv.FormatInt(result.Market.Snapshot.Reading.Sequence, 10) || wire.Market.Snapshot.Reading.Mid != result.Market.Snapshot.Reading.MidPrice) {
		t.Fatal("actual market snapshot lost exact string fields")
	}
	return result.Market
}

func assertNativeMarketPeer(ctx context.Context, t *testing.T, connection grpc.ClientConnInterface, token string) {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if reply, err := strategyv1.NewStrategyMarketQueryClient(connection).ListMarkets(call, &strategyv1.ListMarketsRequest{
		Session: &strategyv1.AdminSession{Token: token},
		Mode:    string(domain.MarketModeReplay),
		Limit:   1,
	}); reply != nil || status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate read management market data", err)
	}
}

func nativeMarketLogin(ctx context.Context, t *testing.T, client *http.Client, base string) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: "control-operator",
		Password: adminFixturePassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", string(data), base, "")
	var body struct {
		Session struct {
			Role     domain.AdminRole `json:"role"`
			Queries  bool             `json:"strategy_queries_enabled"`
			Controls bool             `json:"strategy_controls_enabled"`
		} `json:"session"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(reply.Body, 4096)).Decode(&body)
	closeErr := reply.Body.Close()
	if reply.StatusCode != 200 || decodeErr != nil || closeErr != nil || body.Session.Role != domain.AdminRoleViewer || !body.Session.Queries || body.Session.Controls {
		t.Fatal("actual market login lost viewer/query-only capabilities")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range client.Jar.Cookies(parsed) {
		if cookie.Name == "muskdeer_csrf" {
			return cookie.Value
		}
	}
	t.Fatal("actual market login did not issue independent CSRF")
	return ""
}

func assertNativeMarketPages(ctx context.Context, t *testing.T, client *http.Client, base string, feeds []domain.MarketFeedDefinition) {
	t.Helper()
	var first, last, source, live strategyv1.ListMarketsReply
	path := "/api/v1/markets?mode=REPLAY&limit=1"
	readNativeStrategyProto(ctx, t, client, base, path, &first)
	readNativeStrategyProto(ctx, t, client, base, path+"&after_id="+strconv.FormatInt(feeds[0].ID, 10), &last)
	readNativeStrategyProto(ctx, t, client, base, path+"&source=okx&instrument_id="+strconv.FormatInt(feeds[0].InstrumentID, 10), &source)
	readNativeStrategyProto(ctx, t, client, base, "/api/v1/markets?mode=LIVE", &live)
	if len(first.Markets) != 1 || first.Markets[0].Feed.Id != feeds[0].ID || first.NextAfterId != feeds[0].ID || len(last.Markets) != 1 || last.Markets[0].Feed.Id != feeds[1].ID || last.NextAfterId != 0 || len(source.Markets) != 1 || source.Markets[0].Feed.Source != string(domain.MarketSourceOKX) || source.Markets[0].Snapshot != nil || len(live.Markets) != 0 {
		t.Fatal("actual market HTTP mixed source/mode/product or lost pagination")
	}
}

func assertNativeMarketLogout(ctx context.Context, t *testing.T, client *http.Client, base, csrf string, feedID int64) {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	cookies := client.Jar.Cookies(parsed)
	reply := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/logout", "", base, csrf)
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 204 {
		t.Fatal("actual market viewer logout failed", err)
	}
	for _, path := range []string{"/api/v1/markets?mode=REPLAY", "/api/v1/markets/" + strconv.FormatInt(feedID, 10)} {
		client.Jar.SetCookies(parsed, cookies)
		reply := nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
			t.Fatal("revoked cookie read actual market data", err)
		}
	}
}
