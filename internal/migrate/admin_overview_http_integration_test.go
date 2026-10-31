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

func TestAdminOverviewHTTPReadsFullScopeCountsReleasedLeaseAndUnclaimedSlotWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	viewer := riskActorFixture(ctx, t, db, "overview-viewer", domain.AdminRoleViewer, 91, domain.AdminSessionTTL)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.LeaseKey{
		Kind:       domain.LeaseKindStrategy,
		StrategyID: submit.Input.Authority.StrategyID,
	}
	token, err := leases.Acquire(ctx, key, submit.Input.Authority.OwnerID, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	token, err = leases.Renew(ctx, token, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	adminOverviewRecoverRunning(ctx, t, db, submit, token)
	if err := leases.Release(ctx, token); err != nil {
		t.Fatal(err)
	}
	before := publicationInstance(ctx, t, db, key.StrategyID)
	gridBefore := publicationInstance(ctx, t, db, created.StrategyID)
	riskBefore := riskEventSourceRow(ctx, t, db, key.StrategyID)
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
	assertNativeOverviewPeer(ctx, t, ca, discovery, viewer.Token, key.StrategyID)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "overview-viewer")
	path := "/api/v1/overview/strategies"
	assertNativeOverviewPages(ctx, t, client, base, path, key.StrategyID, created.StrategyID, submit.Input.AccountID)
	var unknown strategyv1.GetStrategyOverviewReply
	readNativeStrategyProto(ctx, t, client, base, path+"?account_id=missing-account", &unknown)
	if unknown.ObservedAtMs <= 0 || unknown.Total != 0 || unknown.RequestedRunning != 0 || len(unknown.StoredStates) != 0 || len(unknown.Items) != 0 || unknown.NextAfterId != 0 {
		t.Fatal("unknown scope fabricated a strategy or lost database observation")
	}
	if err := db.WithContext(ctx).Model(&modelv1.StrategyLease{}).Create(&modelv1.StrategyLease{StrategyInstanceId: created.StrategyID}).Error; err != nil {
		t.Fatal(err)
	}
	var slot strategyv1.GetStrategyOverviewReply
	readNativeStrategyProto(ctx, t, client, base, path+"?requested_market_data_source=okx", &slot)
	if len(slot.Items) != 1 || slot.Items[0].Lease == nil || slot.Items[0].Lease.Epoch != 0 || slot.Items[0].Lease.ExpiresAtMs != 0 ||
		slot.Items[0].Lease.OwnerRecorded || slot.Items[0].Lease.RuntimeMatchesLease || slot.Items[0].Lease.UnexpiredAtObservation {
		t.Fatal("actual zero-epoch lease slot became missing or current ownership")
	}
	assertNativeOverviewErrors(ctx, t, client, base, path)
	assertNativeOverviewLogout(ctx, t, client, jar, base, path)
	if !proto.Equal(before, publicationInstance(ctx, t, db, key.StrategyID)) || !proto.Equal(gridBefore, publicationInstance(ctx, t, db, created.StrategyID)) ||
		!proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, key.StrategyID)) {
		t.Fatal("native overview advanced runtime, configuration or risk")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.StrategyLease{}, 2)
}

func assertNativeOverviewPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, firstID int64) {
	t.Helper()
	request := &strategyv1.GetStrategyOverviewRequest{
		Session: &strategyv1.AdminSession{Token: token},
		Limit:   1,
	}
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	reply, err := strategyv1.NewStrategyOverviewQueryClient(api).GetStrategyOverview(ctx, request)
	if err != nil || reply.Total != 2 || reply.RequestedRunning != 1 || len(reply.Items) != 1 || reply.Items[0].Strategy.Id != firstID ||
		!reply.Items[0].Lease.OwnerRecorded || reply.Items[0].Lease.ExpiresAtMs != 0 || reply.Items[0].Lease.UnexpiredAtObservation || !reply.Items[0].Lease.RuntimeMatchesLease {
		t.Fatal("actual typed observation collapsed released lease and stored runtime", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyOverviewQueryClient(other).GetStrategyOverview(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate read overview", err)
	}
	if _, err := strategyv1.NewStrategyControlClient(api).ControlStrategy(ctx, &strategyv1.ControlStrategyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only node registered control methods", err)
	}
}

func assertNativeOverviewPages(ctx context.Context, t *testing.T, client *http.Client, base, path string, firstID, gridID int64, account string) {
	t.Helper()
	var first, last, empty, selected strategyv1.GetStrategyOverviewReply
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1", &first)
	if first.ObservedAtMs <= 0 || first.Total != 2 || first.RequestedRunning != 1 || len(first.StoredStates) != 2 ||
		len(first.Items) != 1 || first.Items[0].Strategy.Id != firstID || first.NextAfterId != firstID || first.Items[0].Strategy.State != string(domain.StrategyStateRunning) {
		t.Fatal("native overview lost full-scope counts or stored state")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&after_id="+strconv.FormatInt(firstID, 10), &last)
	if last.Total != 2 || last.RequestedRunning != 1 || len(last.Items) != 1 || last.Items[0].Strategy.Id != gridID || last.Items[0].Lease != nil || last.NextAfterId != 0 {
		t.Fatal("native cursor narrowed counts or fabricated a lease")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?after_id=9223372036854775807", &empty)
	if empty.Total != 2 || empty.RequestedRunning != 1 || len(empty.Items) != 0 || empty.NextAfterId != 0 {
		t.Fatal("native exhausted page erased nonempty scope")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?requested_market_data_source=okx&account_id="+url.QueryEscape(account), &selected)
	if selected.Total != 1 || selected.RequestedRunning != 0 || len(selected.Items) != 1 || selected.Items[0].Strategy.Id != gridID || selected.Items[0].Strategy.ActiveMarketDataSource != nil {
		t.Fatal("native exact requested source filter invented an active configuration")
	}
}

func assertNativeOverviewErrors(ctx context.Context, t *testing.T, client *http.Client, base, path string) {
	t.Helper()
	for _, test := range []struct {
		method string
		path   string
		code   int
	}{
		{
			method: "GET",
			path:   path + "?after_id=01",
			code:   400,
		},
		{
			method: "GET",
			path:   path + "?requested_market_data_source=bitget",
			code:   400,
		},
		{
			method: "GET",
			path:   path + "?market_data_source=okx",
			code:   400,
		},
		{
			method: "POST",
			path:   path,
			code:   405,
		},
	} {
		reply := nativeAdminRequest(ctx, t, client, base, test.method, test.path, "", "", "")
		if err := reply.Body.Close(); err != nil || reply.StatusCode != test.code {
			t.Fatal("native overview boundary failed", err, reply.StatusCode)
		}
	}
}

func assertNativeOverviewLogout(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base, path string) {
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
		t.Fatal("actual overview viewer logout failed", err)
	}
	jar.SetCookies(parsed, cookies)
	reply = nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
	if err := reply.Body.Close(); err != nil || reply.StatusCode != 401 {
		t.Fatal("restored revoked cookie read native overview", err, reply.StatusCode)
	}
}
