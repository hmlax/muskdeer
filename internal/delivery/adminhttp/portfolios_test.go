package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const portfolioHTTPPath = "/api/v1/portfolios"

type portfolioHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.ListPortfoliosRequest
	mutate  func(*strategyv1.ListPortfoliosReply)
}

func portfolioHTTPValue() *strategyv1.AccountPortfolio {
	return &strategyv1.AccountPortfolio{
		Id:              9007199254740993,
		ExecutionVenue:  domain.ExecutionVenueSimulator,
		Symbol:          "BTC/USDT",
		AccountId:       "portfolio.account",
		InstrumentId:    1,
		BaseAsset:       "BTC",
		QuoteAsset:      "USDT",
		AccountEnabled:  true,
		AccountRevision: 9007199254740994,
		Projection: &strategyv1.StoredPortfolioProjection{
			OpeningQuantity:  "10",
			OpeningCashQuote: "1000",
			OpeningCostQuote: "1000",
			Quantity:         "10",
			CashQuote:        "1000",
			CostQuote:        "1000",
			RealizedPnlQuote: "-0.123456789012345678901234567890123456",
			FeesQuote:        "0",
			StoredReconciled: true,
			Revision:         1,
		},
		Source: &strategyv1.PortfolioFactProgress{
			Count:                2,
			LatestSourceSequence: 9007199254740995,
		},
		Collected: &strategyv1.PortfolioFactProgress{},
		CoreQuoteBalance: &strategyv1.PortfolioCoreBalance{
			Id:          9007199254740996,
			Asset:       "USDT",
			Available:   "1001.8",
			Frozen:      "0",
			Revision:    3,
			UpdatedAtMs: 1699999999000,
		},
		CreatedAtMs: 1700000000000,
		UpdatedAtMs: 1700000000001,
	}
}

func (c *portfolioHTTPClient) ListPortfolios(ctx context.Context, request *strategyv1.ListPortfoliosRequest, _ ...grpc.CallOption) (*strategyv1.ListPortfoliosReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.ListPortfoliosReply{
		Portfolios:  []*strategyv1.AccountPortfolio{portfolioHTTPValue()},
		NextAfterId: 9007199254740993,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func (c *portfolioHTTPClient) GetPortfolio(ctx context.Context, request *strategyv1.GetPortfolioRequest, _ ...grpc.CallOption) (*strategyv1.GetPortfolioReply, error) {
	page, err := c.ListPortfolios(ctx, &strategyv1.ListPortfoliosRequest{Session: request.Session})
	return &strategyv1.GetPortfolioReply{Portfolio: page.Portfolios[0]}, err
}

func portfolioHTTPFixture(t *testing.T, client strategyv1.StrategyPortfolioQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.portfolios = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestPortfolioHTMLPathsRequireQueryCapabilityAndCanonicalIdentity(t *testing.T) {
	for _, path := range []string{"/portfolios", "/portfolios/9007199254740993"} {
		if reply := perform(portfolioHTTPFixture(t, &portfolioHTTPClient{}), "GET", path, ""); reply.Code != 200 || !strings.Contains(reply.Body.String(), "<html") {
			t.Fatal("portfolio page or independent deep refresh missing", path, reply.Code)
		}
		if reply := perform(portfolioHTTPFixture(t, nil), "GET", path, ""); reply.Code != 404 {
			t.Fatal("disabled portfolio queries exposed HTML", reply.Code)
		}
	}
	for _, path := range []string{"/portfolios/01", "/portfolios/0", "/portfolios/9223372036854775808"} {
		if reply := perform(portfolioHTTPFixture(t, &portfolioHTTPClient{}), "GET", path, ""); reply.Code != 404 {
			t.Fatal("invalid portfolio HTML identity accepted", path, reply.Code)
		}
	}
}

func TestPortfolioHTTPPreservesExactPnLAndIndependentStoredAndCoreFacts(t *testing.T) {
	client := &portfolioHTTPClient{}
	reply := perform(portfolioHTTPFixture(t, client), "GET", portfolioHTTPPath+"?limit=1&after_id=9007199254740992&account_id=portfolio.account&instrument_id=1", "", orderHTTPCookie())
	var body struct {
		Portfolios []struct {
			ID         string `json:"id"`
			Revision   string `json:"account_revision"`
			Projection struct {
				PnL        string `json:"realized_pnl_quote"`
				Cash       string `json:"cash_quote"`
				Reconciled bool   `json:"stored_reconciled"`
			} `json:"projection"`
			CoreBase *struct{} `json:"core_base_balance"`
		} `json:"portfolios"`
		Next string `json:"next_after_id"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 200 || len(body.Portfolios) != 1 || body.Portfolios[0].ID != orderHTTPID || body.Portfolios[0].Revision != "9007199254740994" || body.Portfolios[0].Projection.PnL != "-0.123456789012345678901234567890123456" || body.Portfolios[0].Projection.Cash != "1000" || !body.Portfolios[0].Projection.Reconciled || body.Portfolios[0].CoreBase != nil || body.Next != orderHTTPID || client.request.AccountId != "portfolio.account" || client.request.InstrumentId != 1 || !client.bounded || client.token != testSecret(1) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("portfolio HTTP lost precision, scope, presence or native deadline", err, reply.Code)
	}
}

func TestPortfolioHTTPRejectsAmbiguousScopeCookiesBodiesAndWrites(t *testing.T) {
	client := &portfolioHTTPClient{}
	hertz := portfolioHTTPFixture(t, client)
	for _, path := range []string{
		portfolioHTTPPath + "?account_id=", portfolioHTTPPath + "?account_id=space+key", portfolioHTTPPath + "?account_id=a&account_id=b",
		portfolioHTTPPath + "?instrument_id=0", portfolioHTTPPath + "?instrument_id=01", portfolioHTTPPath + "?instrument_id=1&instrument_id=2",
		portfolioHTTPPath + "?strategy_id=1", portfolioHTTPPath + "?limit=101", portfolioHTTPPath + "?after_id=-1", portfolioHTTPPath + "?limit=01",
		portfolioHTTPPath + "/01", portfolioHTTPPath + "/0", portfolioHTTPPath + "/9223372036854775808", portfolioHTTPPath + "/" + orderHTTPID + "?limit=1",
		portfolioHTTPPath + "?" + strings.Repeat("x", 1025),
	} {
		if reply := perform(hertz, "GET", path, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("ambiguous portfolio request reached downstream", path, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", portfolioHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("portfolio GET body accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", portfolioHTTPPath, ""); reply.Code != 401 {
		t.Fatal("portfolio read without cookie accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", portfolioHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("duplicate portfolio session proof accepted", reply.Code)
	}
	if reply := perform(hertz, "POST", portfolioHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 {
		t.Fatal("read model exposed a write method", reply.Code)
	}
	if reply := perform(portfolioHTTPFixture(t, nil), "GET", portfolioHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled portfolio reads exposed a route", reply.Code)
	}
	if client.calls != 0 {
		t.Fatal("invalid portfolio request touched downstream")
	}
}

func TestPortfolioHTTPRejectsInvalidFinancialOrFilteredSuccessWithoutPartialData(t *testing.T) {
	client := &portfolioHTTPClient{}
	hertz := portfolioHTTPFixture(t, client)
	for _, change := range []func(*strategyv1.ListPortfoliosReply){
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].AccountId = "different.account" },
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].InstrumentId++ },
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].Projection.RealizedPnlQuote = "1e2147483647" },
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].Projection.Quantity = "-1" },
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].Collected.Count = 1 },
		func(r *strategyv1.ListPortfoliosReply) {
			r.Portfolios[0].Source.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].CoreQuoteBalance.Asset = "BTC" },
		func(r *strategyv1.ListPortfoliosReply) { r.NextAfterId++ },
		func(r *strategyv1.ListPortfoliosReply) { r.Portfolios = append(r.Portfolios, r.Portfolios[0]) },
	} {
		client.mutate = change
		reply := perform(hertz, "GET", portfolioHTTPPath+"?limit=1&account_id=portfolio.account&instrument_id=1", "", orderHTTPCookie())
		if reply.Code != 503 || strings.Contains(reply.Body.String(), "1001.8") || strings.Contains(reply.Body.String(), "portfolio.account") {
			t.Fatal("bad portfolio success leaked partial business data", reply.Code)
		}
	}
	client.mutate = func(r *strategyv1.ListPortfoliosReply) { r.Portfolios[0].Id++ }
	if reply := perform(hertz, "GET", portfolioHTTPPath+"/"+orderHTTPID, "", orderHTTPCookie()); reply.Code != 503 {
		t.Fatal("wrong portfolio identity escaped detail binding", reply.Code)
	}
}
