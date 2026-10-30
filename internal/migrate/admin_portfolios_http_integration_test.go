//go:build integration

package migrate

import (
	"context"
	"net/http"
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

func TestAdminPortfolioHTTPReadsStoredDiagnosticsCurrentFundsAndMissingBalancesWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "portfolios-viewer", domain.AdminRoleViewer, 61, domain.AdminSessionTTL)
	other := adminPortfolioWithoutCore(ctx, t, db, submit.Input.InstrumentID)
	core, intents := executionFixture(t, db)
	_, sell := portfolioTrades(ctx, t, core, intents, submit)
	initial := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	strategyBefore := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	assertNativePortfolioPeer(ctx, t, ca, discovery, viewer.Token, initial.Id)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "portfolios-viewer")
	path := "/api/v1/portfolios"
	var stored strategyv1.GetPortfolioReply
	readNativeStrategyProto(ctx, t, client, base, path+"/"+strconv.FormatInt(initial.Id, 10), &stored)
	if !stored.Portfolio.Projection.StoredReconciled || stored.Portfolio.Projection.CashQuote != "1000" || stored.Portfolio.CoreQuoteBalance.Available != "1001.8" || stored.Portfolio.Source.Count != 2 || stored.Portfolio.Collected.Count != 0 || !proto.Equal(initial, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) {
		t.Fatal("actual HTTP reconciled or hid the stale producer diagnostic")
	}
	portfolio, err := repository.NewPortfolio(db)
	if err != nil {
		t.Fatal(err)
	}
	if partial, err := portfolio.Apply(ctx, portfolioNotice(ctx, t, sell)); err != nil || partial.Reconciled || partial.ErrorCode != domain.PortfolioErrorIncomplete {
		t.Fatal("actual later delivery did not remain incomplete", err)
	}
	projection := adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)
	assertNativePortfolioPages(ctx, t, client, base, path, projection, other)
	assertNativePortfolioScope(ctx, t, client, base, path, other.Id+1)
	assertNativeOrderLogout(ctx, t, client, jar, base, path, initial.Id)
	if !proto.Equal(projection, adminTradePortfolioRow(ctx, t, db, submit.Input.AccountID)) || !proto.Equal(other, adminTradePortfolioRow(ctx, t, db, other.AccountId)) || !proto.Equal(strategyBefore, publicationInstance(ctx, t, db, strategyBefore.Id)) {
		t.Fatal("management reads changed producer state, incomplete account or strategy")
	}
	assertCancellationResolutionBudgets(ctx, t, db, submit, "200")
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1001.8", "0")
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioAccount{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 1)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorBalance{}, 2)
	assertGatewayOrderCount(ctx, t, db, 2)
}

func assertNativePortfolioPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, token string, id int64) {
	t.Helper()
	request := &strategyv1.GetPortfolioRequest{
		Session:     &strategyv1.AdminSession{Token: token},
		PortfolioId: id,
	}
	api := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServiceAPI)
	reply, err := strategyv1.NewStrategyPortfolioQueryClient(api).GetPortfolio(ctx, request)
	if err != nil || reply.Portfolio.Id != id || reply.Portfolio.Source.Count != 2 || reply.Portfolio.Collected.Count != 0 || !reply.Portfolio.Projection.StoredReconciled || reply.Portfolio.CoreQuoteBalance.Available != "1001.8" {
		t.Fatal("actual API peer lost independent stored/source/core observations", err)
	}
	other := adminStrategyProcessConnection(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := strategyv1.NewStrategyPortfolioQueryClient(other).GetPortfolio(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatal("valid non-API certificate read management account data", err)
	}
	if _, err := strategyv1.NewStrategyControlClient(api).ControlStrategy(ctx, &strategyv1.ControlStrategyRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatal("query-only node registered strategy writes", err)
	}
}

func assertNativePortfolioPages(ctx context.Context, t *testing.T, client *http.Client, base, path string, primary, other *modelv1.PortfolioAccount) {
	t.Helper()
	var first, last strategyv1.ListPortfoliosReply
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1", &first)
	if len(first.Portfolios) != 1 || first.Portfolios[0].Id != primary.Id || first.NextAfterId != primary.Id || first.Portfolios[0].Projection.StoredReconciled || first.Portfolios[0].Projection.ErrorCode != domain.PortfolioErrorIncomplete || first.Portfolios[0].Source.Count != 2 || first.Portfolios[0].Collected.Count != 1 {
		t.Fatal("actual portfolio page hid explicit producer progress")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?limit=1&after_id="+strconv.FormatInt(first.NextAfterId, 10), &last)
	if len(last.Portfolios) != 1 || last.Portfolios[0].Id != other.Id || last.NextAfterId != 0 || last.Portfolios[0].AccountEnabled || last.Portfolios[0].CoreBaseBalance != nil || last.Portfolios[0].CoreQuoteBalance != nil || last.Portfolios[0].Projection.RealizedPnlQuote != "-0.123456789012345678901234567890123456" || last.Portfolios[0].Source.Count != 0 {
		t.Fatal("actual HTTP rounded loss, borrowed funds or crossed account source scope")
	}
	var filtered strategyv1.ListPortfoliosReply
	readNativeStrategyProto(ctx, t, client, base, path+"?account_id="+url.QueryEscape(other.AccountId)+"&instrument_id="+strconv.FormatInt(other.InstrumentId, 10), &filtered)
	if len(filtered.Portfolios) != 1 || filtered.Portfolios[0].Id != other.Id || filtered.NextAfterId != 0 {
		t.Fatal("actual exact account/instrument filter lost its scope")
	}
}

func assertNativePortfolioScope(ctx context.Context, t *testing.T, client *http.Client, base, path string, missingID int64) {
	t.Helper()
	for _, test := range []struct {
		method string
		path   string
		status int
	}{
		{
			method: "GET",
			path:   path + "/" + strconv.FormatInt(missingID, 10),
			status: 404,
		},
		{
			method: "GET",
			path:   path + "?account_id=missing-account",
			status: 404,
		},
		{
			method: "GET",
			path:   path + "?instrument_id=01",
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
			t.Fatal("actual portfolio HTTP lost scope or read-only boundary", err, reply.StatusCode)
		}
	}
}
