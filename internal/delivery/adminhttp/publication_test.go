package adminhttp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/internal/domain"
)

const publicationHTTPBody = `{"expected_revision":"9007199254740993","expected_config_version":"1","market_data_source":"` + string(domain.MarketSourceOKX) + `","parameters":{"grid":{"lower_price":"98.123456789012345678","upper_price":"102","grid_count":5,"total_budget":"50","per_order_notional":"10","inventory_limit":"20","max_market_age_ms":"500"}},"reason":"  explicit publication  "}`
const publicationHTTPPath = "/api/v1/strategies/9007199254740994/configurations"

type publicationHTTPClient struct {
	strategyHTTPClient
	request *strategyv1.PublishConfigurationRequest
	mutate  func(*strategyv1.PublicationReceipt)
}

func (c *publicationHTTPClient) PublishConfiguration(ctx context.Context, request *strategyv1.PublishConfigurationRequest, _ ...grpc.CallOption) (*strategyv1.PublicationReceipt, error) {
	c.record(ctx, request.Session)
	c.request = request
	reply := &strategyv1.PublicationReceipt{
		Id:                    9007199254740993,
		ActorUserId:           1,
		StrategyId:            request.StrategyId,
		PreviousRevision:      request.Input.ExpectedRevision,
		Revision:              request.Input.ExpectedRevision + 1,
		PreviousConfigVersion: request.Input.ExpectedConfigVersion,
		ConfigVersion:         request.Input.ExpectedConfigVersion + 1,
		MarketDataSource:      request.Input.MarketDataSource,
		Reason:                request.Input.Reason,
		CreatedAtMs:           1700000000000,
	}
	if c.mutate != nil {
		c.mutate(reply)
	}
	return reply, c.failure
}

func publicationHTTPFixture(t *testing.T, client strategyv1.StrategyPublicationClient) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.publication = client
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestPublicationHTTPPreservesLongIdentityDecimalAndIndependentProof(t *testing.T) {
	client := &publicationHTTPClient{}
	hertz := publicationHTTPFixture(t, client)
	reply := perform(hertz, "POST", publicationHTTPPath, publicationHTTPBody, controlHTTPHeaders()...)
	var body struct {
		ID               string `json:"id"`
		StrategyID       string `json:"strategy_id"`
		PreviousRevision string `json:"previous_revision"`
		Revision         string `json:"revision"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &body); err != nil || reply.Code != 201 || body.ID != "9007199254740993" || body.StrategyID != "9007199254740994" || body.PreviousRevision != "9007199254740993" || body.Revision != "9007199254740994" {
		t.Fatal("publication lost exact response identity/revision", err, reply.Code)
	}
	if !client.bounded || client.request.Input.Parameters.GetGrid().LowerPrice != "98.123456789012345678" || client.request.CsrfToken != testSecret(2) || client.request.Session.Token != testSecret(1) || client.request.IdempotencyKey != "http-control-original-key" || client.request.Input.Reason != "explicit publication" || string(reply.Header().Peek("Location")) != "/api/v1/strategies/9007199254740994" {
		t.Fatal("publication lost typed input, owned header or independent proof")
	}
	client.failure = status.Error(codes.Aborted, "private-data")
	if reply := perform(hertz, "POST", publicationHTTPPath, publicationHTTPBody, controlHTTPHeaders()...); reply.Code != 409 || strings.Contains(reply.Body.String(), "private-data") {
		t.Fatal("publication conflict was lost or leaked private cause")
	}
}

func TestPublicationHTTPRejectsAmbiguityAndMissingProofBeforeRPC(t *testing.T) {
	client := &publicationHTTPClient{}
	hertz := publicationHTTPFixture(t, client)
	for _, body := range []string{
		`{}`, `null`, publicationHTTPBody + `{}`,
		strings.Replace(publicationHTTPBody, `"expected_revision":"9007199254740993"`, `"expected_revision":9007199254740993`, 1),
		strings.Replace(publicationHTTPBody, `"expected_config_version":"1"`, `"expected_config_version":1`, 1),
		strings.Replace(publicationHTTPBody, `"expected_config_version":"1"`, `"expected_config_version":"01"`, 1),
		strings.Replace(publicationHTTPBody, `"lower_price":"98.123456789012345678"`, `"lower_price":98.1`, 1),
		strings.Replace(publicationHTTPBody, `"lower_price":"98.123456789012345678"`, `"lower_price":"98","lower_price":"99"`, 1),
		strings.Replace(publicationHTTPBody, `"grid_count":5`, `"grid_count":5,"unknown":true`, 1),
		strings.Replace(publicationHTTPBody, `"reason":`, `"session":"forged","reason":`, 1),
		strings.Repeat(" ", maximumPublicationBytes+1),
	} {
		if reply := perform(hertz, "POST", publicationHTTPPath, body, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("ambiguous publication accepted", reply.Code)
		}
	}
	for index, expected := range []int{403, 401, 403, 400} {
		headers := controlHTTPHeaders()
		headers = append(headers[:index], headers[index+1:]...)
		if reply := perform(hertz, "POST", publicationHTTPPath, publicationHTTPBody, headers...); reply.Code != expected {
			t.Fatal("publication accepted missing independent proof", index, reply.Code)
		}
	}
	for _, path := range []string{publicationHTTPPath + "?unexpected=1", "/api/v1/strategies/01/configurations"} {
		if reply := perform(hertz, "POST", path, publicationHTTPBody, controlHTTPHeaders()...); reply.Code != 400 {
			t.Fatal("publication accepted ambiguous path/query identity", reply.Code)
		}
	}
	if client.calls != 0 {
		t.Fatal("invalid publication caused RPC")
	}
}

func TestPublicationHTTPRejectsMismatchedCommittedReceipt(t *testing.T) {
	for _, mutate := range []func(*strategyv1.PublicationReceipt){
		func(r *strategyv1.PublicationReceipt) { r.StrategyId++ },
		func(r *strategyv1.PublicationReceipt) { r.PreviousRevision-- },
		func(r *strategyv1.PublicationReceipt) { r.Revision++ },
		func(r *strategyv1.PublicationReceipt) { r.PreviousConfigVersion++ },
		func(r *strategyv1.PublicationReceipt) { r.ConfigVersion = 1 },
		func(r *strategyv1.PublicationReceipt) { r.MarketDataSource = string(domain.MarketSourceBinance) },
		func(r *strategyv1.PublicationReceipt) { r.Reason = "different" },
		func(r *strategyv1.PublicationReceipt) { r.ActorUserId = 0 },
		func(r *strategyv1.PublicationReceipt) { r.CreatedAtMs = 0 },
	} {
		client := &publicationHTTPClient{mutate: mutate}
		reply := perform(publicationHTTPFixture(t, client), "POST", publicationHTTPPath, publicationHTTPBody, controlHTTPHeaders()...)
		if reply.Code != 503 || len(reply.Header().Peek("Location")) != 0 {
			t.Fatal("publication exposed a mismatched success receipt", reply.Code)
		}
	}
}
