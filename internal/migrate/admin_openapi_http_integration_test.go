//go:build integration

package migrate

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/adminopenapi"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
	"github.com/hmlax/muskdeer/internal/testopenapi"
)

func TestAdminOpenAPIHTTPAuthorizesDownloadAndValidatesNativeQueryPayloadsWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	actor, creator, submit := adminCreationFixture(ctx, t, db)
	created, err := creator.Create(ctx, actor.Token, actor.CSRF, adminCreationRequest(t, submit, domain.StrategyTemplateGrid, domain.MarketSourceOKX))
	if err != nil {
		t.Fatal(err)
	}
	riskActorFixture(ctx, t, db, "openapi-viewer", domain.AdminRoleViewer, 121, domain.AdminSessionTTL)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	gridBefore := publicationInstance(ctx, t, db, created.StrategyID)
	riskBefore := riskEventSourceRow(ctx, t, db, before.Id)
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
	strategy := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	strategy.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, strategy, discovery)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	unauthenticated := nativeAdminRequest(ctx, t, client, base, "GET", "/openapi.json", "", "", "")
	if unauthenticated.StatusCode != 401 {
		t.Fatal("actual document download was anonymous")
	}
	nativeOpenAPIBody(t, unauthenticated, 4096)
	login := nativeAdminRequest(ctx, t, client, base, "POST", "/api/v1/auth/login", `{"username":"openapi-viewer","password":"`+adminFixturePassword+`"}`, base, "")
	loginBody := nativeOpenAPIBody(t, login, 4096)
	if login.StatusCode != 200 {
		t.Fatal("actual viewer login failed")
	}
	download := nativeAdminRequest(ctx, t, client, base, "GET", "/openapi.json", "", "", "")
	document := nativeOpenAPIBody(t, download, 512*1024)
	want, err := adminopenapi.Generate()
	if err != nil || download.StatusCode != 200 || !bytes.Equal(document, want) {
		t.Fatal("actual API document differed from checked generated contract", err)
	}
	contract, err := testopenapi.New(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.ValidateResponse("POST", "/api/v1/auth/login", login.StatusCode, loginBody); err != nil {
		t.Fatal(err)
	}
	nativeOpenAPIQueries(ctx, t, client, base, contract, before.Id, created.StrategyID, submit.Input.InstrumentID)
	nativeOpenAPIDetails(ctx, t, client, base, contract)
	nativeOpenAPIAbsentResources(ctx, t, client, base, contract, before.Id)
	nativeOpenAPILogout(ctx, t, client, jar, base, contract)
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/auth/session", "/api/v1/auth/session", 401)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(gridBefore, publicationInstance(ctx, t, db, created.StrategyID)) ||
		!proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, before.Id)) {
		t.Fatal("document or contract observation changed strategy/risk")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
}

func nativeOpenAPIQueries(ctx context.Context, t *testing.T, client *http.Client, base string, contract *testopenapi.Contract, strategyID, gridID, instrumentID int64) {
	t.Helper()
	strategyPath := "/api/v1/strategies/" + strconv.FormatInt(strategyID, 10)
	for _, item := range []struct {
		path     string
		template string
	}{
		{
			path:     "/api/v1/auth/session",
			template: "/api/v1/auth/session",
		},
		{
			path:     "/api/v1/strategies?limit=1",
			template: "/api/v1/strategies",
		},
		{
			path:     strategyPath,
			template: "/api/v1/strategies/{strategyID}",
		},
		{
			path:     "/api/v1/strategies/" + strconv.FormatInt(gridID, 10),
			template: "/api/v1/strategies/{strategyID}",
		},
		{
			path:     strategyPath + "/versions",
			template: "/api/v1/strategies/{strategyID}/versions",
		},
		{
			path:     "/api/v1/accounts",
			template: "/api/v1/accounts",
		},
		{
			path:     "/api/v1/instruments",
			template: "/api/v1/instruments",
		},
		{
			path:     "/api/v1/instruments/" + strconv.FormatInt(instrumentID, 10) + "/market-feeds?mode=" + string(domain.MarketModeReplay),
			template: "/api/v1/instruments/{instrumentID}/market-feeds",
		},
		{
			path:     strategyPath + "/orders",
			template: "/api/v1/strategies/{strategyID}/orders",
		},
		{
			path:     strategyPath + "/trades",
			template: "/api/v1/strategies/{strategyID}/trades",
		},
		{
			path:     "/api/v1/risk-events",
			template: "/api/v1/risk-events",
		},
		{
			path:     "/api/v1/overview/strategies",
			template: "/api/v1/overview/strategies",
		},
		{
			path:     "/api/v1/overview/services",
			template: "/api/v1/overview/services",
		},
		{
			path:     "/api/v1/overview/services?role=api",
			template: "/api/v1/overview/services",
		},
		{
			path:     "/api/v1/overview/services?role=cron",
			template: "/api/v1/overview/services",
		},
		{
			path:     strategyPath + "/risk",
			template: "/api/v1/strategies/{strategyID}/risk",
		},
	} {
		data := nativeOpenAPIRead(ctx, t, client, base, contract, item.path, item.template, 200)
		if item.path == "/api/v1/strategies/"+strconv.FormatInt(gridID, 10) {
			nativeOpenAPIRejectsNumericCopies(t, contract, data, gridID)
		}
	}
}

func nativeOpenAPIRejectsNumericCopies(t *testing.T, contract *testopenapi.Contract, data []byte, gridID int64) {
	t.Helper()
	var reply strategyv1.GetStrategyReply
	if err := protojson.Unmarshal(data, &reply); err != nil || reply.GetRequestedConfig().GetParameters().GetGrid() == nil {
		t.Fatal("native typed grid payload absent", err)
	}
	for _, field := range []struct {
		original string
		replaced string
	}{
		{
			original: `"id":"` + strconv.FormatInt(gridID, 10) + `"`,
			replaced: `"id":` + strconv.FormatInt(gridID, 10),
		},
		{
			original: `"lower_price":` + strconv.Quote(reply.RequestedConfig.Parameters.GetGrid().LowerPrice),
			replaced: `"lower_price":1`,
		},
	} {
		// Deliberately damaged copies test the validator, not the real API.
		damaged := bytes.Replace(data, []byte(field.original), []byte(field.replaced), 1)
		if bytes.Equal(data, damaged) || contract.ValidateResponse("GET", "/api/v1/strategies/{strategyID}", 200, damaged) == nil {
			t.Fatal("downloaded schema did not reject a numeric protocol copy")
		}
	}
}

func nativeOpenAPIDetails(ctx context.Context, t *testing.T, client *http.Client, base string, contract *testopenapi.Contract) {
	t.Helper()
	var audits strategyv1.ListAuditsReply
	data := nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/audits", "/api/v1/audits", 200)
	if err := protojson.Unmarshal(data, &audits); err != nil || len(audits.Audits) == 0 {
		t.Fatal("fixture missing actual audit", err)
	}
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/audits/"+strconv.FormatInt(audits.Audits[0].Id, 10), "/api/v1/audits/{auditID}", 200)
	var portfolios strategyv1.ListPortfoliosReply
	data = nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/portfolios", "/api/v1/portfolios", 200)
	if err := protojson.Unmarshal(data, &portfolios); err != nil || len(portfolios.Portfolios) == 0 {
		t.Fatal("fixture missing actual initialized portfolio", err)
	}
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/portfolios/"+strconv.FormatInt(portfolios.Portfolios[0].Id, 10), "/api/v1/portfolios/{portfolioID}", 200)
	var markets strategyv1.ListMarketsReply
	data = nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/markets?mode="+string(domain.MarketModeReplay), "/api/v1/markets", 200)
	if err := protojson.Unmarshal(data, &markets); err != nil || len(markets.Markets) != 2 || markets.Markets[0].Feed == nil {
		t.Fatal("fixture missing two independent source feeds", err)
	}
	for _, market := range markets.Markets {
		nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/markets/"+strconv.FormatInt(market.Feed.Id, 10), "/api/v1/markets/{feedID}", 200)
	}
}

func nativeOpenAPIAbsentResources(ctx context.Context, t *testing.T, client *http.Client, base string, contract *testopenapi.Contract, strategyID int64) {
	t.Helper()
	prefix := "/api/v1/strategies/" + strconv.FormatInt(strategyID, 10)
	for _, suffix := range []string{"orders", "trades", "order-cancellations"} {
		parameter := "orderID"
		switch suffix {
		case "trades":
			parameter = "tradeID"
		case "order-cancellations":
			parameter = "cancellationID"
		}
		nativeOpenAPIRead(ctx, t, client, base, contract, prefix+"/"+suffix+"/9223372036854775807", "/api/v1/strategies/{strategyID}/"+suffix+"/{"+parameter+"}", 404)
	}
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/risk-events/9223372036854775807", "/api/v1/risk-events/{eventID}", 404)
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/operations/1", "/api/v1/operations/{operationID}", 404)
	nativeOpenAPIRead(ctx, t, client, base, contract, "/api/v1/markets", "/api/v1/markets", 400)
}

func nativeOpenAPIRead(ctx context.Context, t *testing.T, client *http.Client, base string, contract *testopenapi.Contract, path, template string, status int) []byte {
	t.Helper()
	response := nativeAdminRequest(ctx, t, client, base, "GET", path, "", "", "")
	data := nativeOpenAPIBody(t, response, 256*1024)
	if response.StatusCode != status {
		t.Fatalf("native %s status %d, want %d", path, response.StatusCode, status)
	}
	if err := contract.ValidateResponse("GET", template, status, data); err != nil {
		t.Fatalf("native %s violated downloaded contract: %v", path, err)
	}
	return data
}

func nativeOpenAPIBody(t *testing.T, response *http.Response, maximum int64) []byte {
	t.Helper()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || int64(len(data)) > maximum ||
		response.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatal("actual HTTP lost bounded JSON/no-store response", readErr, closeErr)
	}
	return data
}

func nativeOpenAPILogout(ctx context.Context, t *testing.T, client *http.Client, jar *cookiejar.Jar, base string, contract *testopenapi.Contract) {
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
	data, readErr := io.ReadAll(io.LimitReader(reply.Body, 65))
	closeErr := reply.Body.Close()
	if reply.StatusCode != 204 || reply.Header.Get("Cache-Control") != "no-store" || readErr != nil || closeErr != nil {
		t.Fatal("actual logout lost its bodyless no-store contract", readErr, closeErr)
	}
	if err := contract.ValidateResponse("POST", "/api/v1/auth/logout", 204, data); err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(parsed, cookies)
	reply = nativeAdminRequest(ctx, t, client, base, "GET", "/openapi.json", "", "", "")
	data = nativeOpenAPIBody(t, reply, 4096)
	if reply.StatusCode != 401 || bytes.Contains(data, []byte(`"openapi"`)) {
		t.Fatal("restored revoked cookie downloaded documentation")
	}
}
