//go:build integration

package migrate

import (
	"context"
	"net/http"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	adminv1 "github.com/hmlax/muskdeer/api/admin/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminServicesHTTPReadsRealNamedReadinessUnconfiguredMembershipAndCurrentAPIWithoutWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	riskActorFixture(ctx, t, db, "services-http-viewer", domain.AdminRoleViewer, 111, domain.AdminSessionTTL)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
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
	a := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	a.InstanceId, a.Version = "service-http-a", "declared-example-a"
	a.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, a, discovery)
	b := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	b.InstanceId, b.Version = "service-http-b", "declared-example-b"
	b.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	bPID := startBusinessProcess(ctx, t, b, discovery)
	// Standard Deer role only: no market worker, public source or trading claim.
	market := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceMarket))
	market.Business = nil
	market.InstanceId, market.Version = "service-http-market", "declared-base-market-example"
	startBusinessProcess(ctx, t, market, discovery)
	base := startStrategyQueryAPI(ctx, t, ca, postgres, naming)
	client, jar := nativeRiskHTTPClient(t)
	nativeCreationLogin(ctx, t, client, jar, base, "services-http-viewer")
	path := "/api/v1/overview/services"
	assertNativeServiceHTTPPages(ctx, t, client, base, path, a, b)
	assertNativeServiceHTTPScopes(ctx, t, client, base, path, market)
	assertNativeServiceHTTPErrors(ctx, t, client, base, path)
	if err := syscall.Kill(bPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitServiceObserverRemoval(ctx, t, discovery, b.InstanceId)
	var current adminv1.ServiceOverview
	readNativeStrategyProto(ctx, t, client, base, path+"?role=strategy", &current)
	if current.Total != 1 || len(current.Items) != 1 || current.Items[0].InstanceId != a.InstanceId || current.Items[0].Status != string(domain.AdminServiceReady) || current.NextAfterInstanceId != "" {
		t.Fatal("actual HTTP cached a departed process or lost the independent peer")
	}
	assertNativeOverviewLogout(ctx, t, client, jar, base, path)
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, before.Id)) {
		t.Fatal("native service HTTP changed runtime or risk")
	}
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
}

func assertNativeServiceHTTPPages(ctx context.Context, t *testing.T, client *http.Client, base, path string, a, b *configv1.ServiceConfig) {
	t.Helper()
	var first, last, empty adminv1.ServiceOverview
	readNativeStrategyProto(ctx, t, client, base, path+"?role=strategy&limit=1", &first)
	if first.Role != string(domain.AdminServiceStrategy) || first.MembershipSource != string(domain.AdminServiceRegistry) || first.MembershipObservedAtMs <= 0 ||
		!first.ProbeConfigured || first.Total != 2 || len(first.Items) != 1 || first.Items[0].InstanceId != a.InstanceId || first.Items[0].DeclaredVersion != a.Version ||
		first.Items[0].Status != string(domain.AdminServiceReady) || first.Items[0].ProbedAtMs == nil || *first.Items[0].ProbedAtMs <= 0 || first.NextAfterInstanceId != a.InstanceId {
		t.Fatal("actual HTTP lost named readiness, source or declared metadata")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?role=strategy&limit=1&after_instance_id="+a.InstanceId, &last)
	if last.Total != 2 || len(last.Items) != 1 || last.Items[0].InstanceId != b.InstanceId || last.Items[0].DeclaredVersion != b.Version || last.Items[0].Status != string(domain.AdminServiceReady) || last.NextAfterInstanceId != "" {
		t.Fatal("actual HTTP cursor changed full membership counts or returned another instance")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?role=strategy&after_instance_id="+b.InstanceId, &empty)
	if empty.Total != 2 || len(empty.Items) != 0 || empty.NextAfterInstanceId != "" {
		t.Fatal("exhausted actual service page erased discovered members")
	}
}

func assertNativeServiceHTTPScopes(ctx context.Context, t *testing.T, client *http.Client, base, path string, market *configv1.ServiceConfig) {
	t.Helper()
	var local, unconfigured, empty adminv1.ServiceOverview
	readNativeStrategyProto(ctx, t, client, base, path+"?role=api", &local)
	if local.Role != string(domain.AdminServiceAPI) || local.MembershipSource != string(domain.AdminServiceLocalAPI) || local.Total != 1 || len(local.Items) != 1 ||
		local.Items[0].InstanceId != "strategy-query-api" || local.Items[0].Status != string(domain.AdminServiceReady) || local.Items[0].ProbedAtMs == nil {
		t.Fatal("current API health was replaced with all-API registry membership")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?role=market", &unconfigured)
	if unconfigured.ProbeConfigured || unconfigured.Total != 1 || len(unconfigured.Items) != 1 || unconfigured.Items[0].InstanceId != market.InstanceId ||
		unconfigured.Items[0].DeclaredVersion != market.Version || unconfigured.Items[0].Status != string(domain.AdminServiceProbeNotConfigured) || unconfigured.Items[0].ProbedAtMs != nil {
		t.Fatal("HTTP silently expanded permissions or fabricated a market probe")
	}
	readNativeStrategyProto(ctx, t, client, base, path+"?role=cron", &empty)
	if empty.Role != string(domain.AdminServiceCron) || empty.MembershipSource != string(domain.AdminServiceRegistry) || empty.MembershipObservedAtMs <= 0 ||
		empty.ProbeConfigured || empty.Total != 0 || len(empty.Items) != 0 || empty.NextAfterInstanceId != "" {
		t.Fatal("empty unconfigured role became ready or a fabricated instance")
	}
}

func assertNativeServiceHTTPErrors(ctx context.Context, t *testing.T, client *http.Client, base, path string) {
	t.Helper()
	for _, test := range []struct {
		method string
		path   string
		code   int
	}{
		{
			method: "GET",
			path:   path + "?role=strategy.grpc",
			code:   400,
		},
		{
			method: "GET",
			path:   path + "?limit=01",
			code:   400,
		},
		{
			method: "GET",
			path:   path + "?role=api&role=strategy",
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
			t.Fatal("service HTTP request boundary failed", err, reply.StatusCode)
		}
	}
}
