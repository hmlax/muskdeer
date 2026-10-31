package adminhttp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/hmlax/muskdeer/internal/domain"
)

const servicesHTTPPath = "/api/v1/overview/services"

type servicesHTTPReader struct {
	calls   int
	token   string
	filter  domain.AdminServiceFilter
	bounded bool
	mutate  func(*domain.AdminServiceOverview)
	err     error
}

func (s *servicesHTTPReader) Observe(ctx context.Context, token string, filter domain.AdminServiceFilter) (domain.AdminServiceOverview, error) {
	s.calls++
	s.token, s.filter = token, filter
	deadline, ok := ctx.Deadline()
	s.bounded = ok && time.Until(deadline) <= strategyReadTimeout
	checked := time.UnixMilli(9007199254740994)
	value := domain.AdminServiceOverview{
		Role:                 domain.AdminServiceStrategy,
		Source:               domain.AdminServiceRegistry,
		MembershipObservedAt: time.UnixMilli(9007199254740993),
		Total:                2,
		ProbeConfigured:      true,
		Items: []domain.AdminServiceInstance{{
			InstanceID:      "node-a",
			DeclaredVersion: "declared-example",
			Status:          domain.AdminServiceProbeFailed,
			ProbedAt:        &checked,
		}},
		NextAfterInstanceID: "node-a",
	}
	if s.mutate != nil {
		s.mutate(&value)
	}
	return value, s.err
}

func servicesHTTPFixture(t *testing.T, reader domain.AdminServices) *server.Hertz {
	t.Helper()
	routes, err := New(&authFixture{}, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	routes.services = reader
	hertz := server.New()
	routes.Register(hertz)
	return hertz
}

func TestServicesHTTPKeepsExactSampleTimesAndExplicitProbeFailure(t *testing.T) {
	reader := &servicesHTTPReader{}
	response := perform(servicesHTTPFixture(t, reader), "GET", servicesHTTPPath+"?role=strategy&limit=1&after_instance_id=node-0", "", orderHTTPCookie())
	var body struct {
		Role   string `json:"role"`
		Total  string `json:"total"`
		Source string `json:"membership_source"`
		Time   string `json:"membership_observed_at_ms"`
		Items  []struct {
			ID     string `json:"instance_id"`
			Status string `json:"status"`
			Time   string `json:"probed_at_ms"`
		} `json:"items"`
		Next string `json:"next_after_instance_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != 200 || body.Role != string(domain.AdminServiceStrategy) || body.Total != "2" ||
		body.Source != string(domain.AdminServiceRegistry) || body.Time != "9007199254740993" || len(body.Items) != 1 || body.Items[0].ID != "node-a" ||
		body.Items[0].Status != string(domain.AdminServiceProbeFailed) || body.Items[0].Time != "9007199254740994" || body.Next != "node-a" ||
		!reader.bounded || reader.token != testSecret(1) || reader.filter.AfterInstanceID != "node-0" || reader.filter.Limit != 1 || string(response.Header().Peek("Cache-Control")) != "no-store" {
		t.Fatal("HTTP service observation lost precise times, scope, session or diagnostic", response.Code, err)
	}
}

func TestServicesHTTPUnconfiguredProbeOmitsOptionalTimestamp(t *testing.T) {
	reader := &servicesHTTPReader{mutate: func(value *domain.AdminServiceOverview) {
		value.ProbeConfigured = false
		value.Items[0].Status, value.Items[0].ProbedAt = domain.AdminServiceProbeNotConfigured, nil
	}}
	reply := perform(servicesHTTPFixture(t, reader), "GET", servicesHTTPPath+"?limit=1", "", orderHTTPCookie())
	if reply.Code != 200 || strings.Contains(reply.Body.String(), "probed_at_ms") || !strings.Contains(reply.Body.String(), string(domain.AdminServiceProbeNotConfigured)) || reader.filter.Role != domain.AdminServiceStrategy {
		t.Fatal("unconfigured probe gained a null/zero timestamp or lost default role", reply.Code)
	}
}

func TestServicesHTTPRejectsArbitraryRolesAmbiguousQueriesBodiesCookiesAndWrites(t *testing.T) {
	reader := &servicesHTTPReader{}
	hertz := servicesHTTPFixture(t, reader)
	for _, query := range []string{
		"?role=", "?role=strategy.grpc", "?role=STRATEGY", "?role=http://127.0.0.1:90", "?role=strategy&role=api", "?limit=01", "?limit=0", "?limit=5",
		"?after_instance_id=", "?after_instance_id=node-a&after_instance_id=node-b", "?after_instance_id=+node", "?after_id=1", "?bad=%zz", "?unexpected=1", "?" + strings.Repeat("a", 1025),
	} {
		if reply := perform(hertz, "GET", servicesHTTPPath+query, "", orderHTTPCookie()); reply.Code != 400 {
			t.Fatal("invalid service selector reached the observer", query, reply.Code)
		}
	}
	if reply := perform(hertz, "GET", servicesHTTPPath, "{}", orderHTTPCookie()); reply.Code != 400 {
		t.Fatal("GET body accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", servicesHTTPPath, ""); reply.Code != 401 {
		t.Fatal("anonymous service observation accepted", reply.Code)
	}
	if reply := perform(hertz, "GET", servicesHTTPPath, "", orderHTTPCookie(), orderHTTPCookie()); reply.Code != 401 {
		t.Fatal("ambiguous cookie proof accepted", reply.Code)
	}
	if reply := perform(hertz, "POST", servicesHTTPPath, "{}", orderHTTPCookie()); reply.Code != 405 {
		t.Fatal("service query exposed a write method", reply.Code)
	}
	if reply := perform(servicesHTTPFixture(t, nil), "GET", servicesHTTPPath, "", orderHTTPCookie()); reply.Code != 404 {
		t.Fatal("disabled service query exposed a route", reply.Code)
	}
	if reader.calls != 0 {
		t.Fatal("invalid service queries touched dependencies")
	}
}

func TestServicesHTTPDiscardsMalformedSuccessesAndPrivateFailures(t *testing.T) {
	reader := &servicesHTTPReader{}
	hertz := servicesHTTPFixture(t, reader)
	for _, change := range []func(*domain.AdminServiceOverview){
		func(v *domain.AdminServiceOverview) { v.Role = domain.AdminServiceMarket },
		func(v *domain.AdminServiceOverview) { v.Total = 0 },
		func(v *domain.AdminServiceOverview) { v.Items[0].Status = "unknown" },
		func(v *domain.AdminServiceOverview) { v.ProbeConfigured = false },
		func(v *domain.AdminServiceOverview) { v.Items[0].ProbedAt = nil },
		func(v *domain.AdminServiceOverview) { v.NextAfterInstanceID = "other" },
		func(v *domain.AdminServiceOverview) { v.MembershipObservedAt = time.Time{} },
	} {
		reader.mutate = change
		reply := perform(hertz, "GET", servicesHTTPPath+"?limit=1", "", orderHTTPCookie())
		if reply.Code != 503 || strings.Contains(reply.Body.String(), "node-a") || strings.Contains(reply.Body.String(), "declared-example") {
			t.Fatal("invalid service success exposed partial members", reply.Code)
		}
	}
	reader.mutate = nil
	for _, test := range []struct {
		err  error
		code int
	}{
		{
			err:  errors.New("private registry address"),
			code: 503,
		},
		{
			err:  errors.Join(domain.ErrUnauthenticated, errors.New("private session details")),
			code: 401,
		},
		{
			err:  errors.Join(domain.ErrForbidden, errors.New("private role details")),
			code: 403,
		},
	} {
		reader.err = test.err
		reply := perform(hertz, "GET", servicesHTTPPath+"?limit=1", "", orderHTTPCookie())
		if reply.Code != test.code || strings.Contains(reply.Body.String(), "private") || strings.Contains(reply.Body.String(), "node-a") {
			t.Fatal("private failure escaped or exposed partial members", reply.Code)
		}
	}
}
