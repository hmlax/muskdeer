package adminhttp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const overviewHTTPPath = "/api/v1/overview/strategies"

type overviewHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.GetStrategyOverviewRequest
	mutate  func(*strategyv1.GetStrategyOverviewReply)
}

func overviewHTTPValue() *strategyv1.GetStrategyOverviewReply {
	return &strategyv1.GetStrategyOverviewReply{
		ObservedAtMs:     2000,
		Total:            9007199254740993,
		RequestedRunning: 1,
		StoredStates: []*strategyv1.StoredStrategyStateCount{{
			State: string(domain.StrategyStateRunning),
			Count: 9007199254740993,
		}},
		Items: []*strategyv1.StrategyRuntimeObservation{{
			Strategy: &strategyv1.Strategy{
				Id:                        9007199254740994,
				InstanceKey:               "overview-example",
				AccountId:                 "overview.account",
				InstrumentId:              1,
				Template:                  string(domain.StrategyTemplateGrid),
				CodeVersion:               "test-example",
				Revision:                  9007199254740995,
				State:                     string(domain.StrategyStateRunning),
				DesiredState:              string(domain.StrategyStateStopped),
				RequestedConfigVersion:    2,
				RequestedMarketDataSource: string(domain.MarketSourceOKX),
				ActiveConfigVersion:       proto.Int64(1),
				ActiveMarketDataSource:    proto.String(string(domain.MarketSourceBinance)),
				MarketMode:                string(domain.MarketModeReplay),
				RuntimeOwnerId:            "legacy-holder",
				RuntimeOwnerEpoch:         9007199254740996,
				CreatedAtMs:               1,
				UpdatedAtMs:               1000,
			},
			Lease: &strategyv1.StrategyLeaseObservation{
				Epoch:               9007199254740996,
				OwnerRecorded:       true,
				RuntimeMatchesLease: true,
			},
		}},
		NextAfterId: 9007199254740994,
	}
}

func (c *overviewHTTPClient) GetStrategyOverview(ctx context.Context, request *strategyv1.GetStrategyOverviewRequest, _ ...grpc.CallOption) (*strategyv1.GetStrategyOverviewReply, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := overviewHTTPValue()
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func overviewHTTPFixture(t *testing.T, client strategyv1.StrategyOverviewQueryClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.overview = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestOverviewHTTPKeepsHighCountersSeparateSourcesAndReleasedLease(t *testing.T) {
	client := &overviewHTTPClient{}
	path := overviewHTTPPath + "?limit=1&after_id=9007199254740993&account_id=overview.account&requested_market_data_source=okx"
	reply := perform(overviewHTTPFixture(t, client), "GET", path, "", orderHTTPCookie())
	var body struct {
		Total string `json:"total"`
		Items []struct {
			Strategy struct {
				ID string `json:"id"`
			} `json:"strategy"`
			Lease struct {
				Epoch   string `json:"epoch"`
				Expires string `json:"expires_at_ms"`
				Owner   bool   `json:"owner_recorded"`
			} `json:"lease"`
		} `json:"items"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 200 || body.Total != "9007199254740993" || len(body.Items) != 1 ||
		body.Items[0].Strategy.ID != "9007199254740994" || body.Items[0].Lease.Epoch != "9007199254740996" || body.Items[0].Lease.Expires != "0" || !body.Items[0].Lease.Owner ||
		client.request.AfterId != 9007199254740993 || client.request.RequestedMarketDataSource != "okx" || client.request.AccountId != "overview.account" ||
		!client.bounded || client.token != testSecret(1) || string(reply.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("HTTP overview lost exact counts, lease facts or native boundary", reply.Code, err)
	}
}

func TestOverviewHTTPRejectsAmbiguousFiltersBodiesCookiesAndWrites(t *testing.T) {
	client := &overviewHTTPClient{}
	hertz := overviewHTTPFixture(t, client)
	for _, suffix := range []string{
		"?account_id=", "?account_id=a&account_id=b", "?account_id=bad+account", "?requested_market_data_source=bitget", "?requested_market_data_source=",
		"?requested_market_data_source=okx&requested_market_data_source=binance", "?market_data_source=okx", "?limit=01", "?limit=0", "?limit=101",
		"?after_id=-1", "?after_id=01", "?after_id=9223372036854775808", "?bad=%zz", "?unexpected=1", "?" + strings.Repeat("a", 1025),
	} {
		if reply := perform(hertz, "GET", overviewHTTPPath+suffix, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("ambiguous overview reached downstream", suffix, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", overviewHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("GET body accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", overviewHTTPPath, ""); reply.Code != 401 {
		t.Fatal("anonymous overview accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", overviewHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("duplicate cookie accepted", reply.Code)
	}
	if reply := perform(hertz, "POST", overviewHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 {
		t.Fatal("overview exposed writes", reply.Code)
	}
	if reply := perform(overviewHTTPFixture(t, nil), "GET", overviewHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled overview registered", reply.Code)
	}
	if client.calls != 0 {
		t.Fatal("invalid requests touched RPC")
	}
}

func TestOverviewHTTPRejectsInconsistentCountsLeasesAndNestedUnknownSuccess(t *testing.T) {
	client := &overviewHTTPClient{}
	hertz := overviewHTTPFixture(t, client)
	path := overviewHTTPPath + "?limit=1&account_id=overview.account&requested_market_data_source=okx"
	for i, change := range []func(*strategyv1.GetStrategyOverviewReply){
		func(r *strategyv1.GetStrategyOverviewReply) { r.ObservedAtMs = 0 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Total = -1 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Total-- },
		func(r *strategyv1.GetStrategyOverviewReply) { r.RequestedRunning = r.Total + 1 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.RequestedRunning = -1 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.StoredStates[0].Count = 0 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.StoredStates[0].State = "unknown" },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.StoredStates = append(r.StoredStates, r.StoredStates[0])
		},
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.StoredStates[0].Count = math.MaxInt64
			r.StoredStates = append(r.StoredStates, &strategyv1.StoredStrategyStateCount{
				State: string(domain.StrategyStateStopped),
				Count: 1,
			})
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Strategy.AccountId = "other" },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.Items[0].Strategy.RequestedMarketDataSource = string(domain.MarketSourceBinance)
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Strategy.ActiveMarketDataSource = nil },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Strategy.ActiveConfigVersion = proto.Int64(3) },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Strategy.Id = 0 },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.Items[0].Strategy.State = string(domain.StrategyStateCreated)
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Strategy.RuntimeOwnerId = "" },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.Items[0].Strategy.DesiredState = string(domain.StrategyStateRunning)
			r.RequestedRunning = 0
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.RequestedRunning = r.Total },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Lease.Epoch++ },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Lease.OwnerRecorded = false },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Lease.ExpiresAtMs = -1 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Lease.UnexpiredAtObservation = true },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].Lease.ExpiresAtMs = r.ObservedAtMs + 1 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.NextAfterId++ },
		func(r *strategyv1.GetStrategyOverviewReply) { r.NextAfterId = 0 },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items = nil },
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items = append(r.Items, r.Items[0]) },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.Items[0].Lease.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.Items[0].Strategy.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.Items[0].ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
		func(r *strategyv1.GetStrategyOverviewReply) {
			r.StoredStates[0].ProtoReflect().SetUnknown([]byte{0xf8, 7, 1})
		},
		func(r *strategyv1.GetStrategyOverviewReply) { r.ProtoReflect().SetUnknown([]byte{0xf8, 7, 1}) },
	} {
		client.mutate = change
		reply := perform(hertz, "GET", path, "", orderHTTPCookie())
		if reply.Code != 503 || strings.Contains(reply.Body.String(), "overview.account") || strings.Contains(reply.Body.String(), "9007199254740993") {
			t.Fatal("malformed overview leaked partial successful data", i, reply.Code)
		}
	}
}

func TestOverviewHTTPMapsFailuresWithoutCountersOrPrivateText(t *testing.T) {
	client := &overviewHTTPClient{}
	hertz := overviewHTTPFixture(t, client)
	for _, test := range []struct {
		err  error
		code int
	}{
		{
			err:  errors.New("private database details"),
			code: 503,
		},
		{
			err:  status.Error(codes.Unauthenticated, "private session details"),
			code: 401,
		},
		{
			err:  status.Error(codes.PermissionDenied, "private role details"),
			code: 403,
		},
	} {
		client.failure = test.err
		reply := perform(hertz, "GET", overviewHTTPPath+"?limit=1", "", orderHTTPCookie())
		if reply.Code != test.code || strings.Contains(reply.Body.String(), "private") || strings.Contains(reply.Body.String(), "total") {
			t.Fatal("overview failed to discard private/partial response", reply.Code)
		}
	}
}
