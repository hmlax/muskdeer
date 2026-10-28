//go:build integration

package migrate

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestManagementStrategyProcessServesRealViewerWithoutTradingWorkers(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, auth, login, instances := adminStrategiesFixture(ctx, t, db)
	ca := testcert.New(t)
	naming := businessNaming()
	observer, err := nacos.New(ctx, naming)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observer.Close(); err != nil {
			t.Error(err)
		}
	})
	cfg := businessProcessConfig(t, ca, postgres, nil, naming, "strategy")
	cfg.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	pid := startBusinessProcess(ctx, t, cfg, observer)
	if pid <= 0 || pid == os.Getpid() {
		t.Fatal("management did not start an independent strategy process")
	}
	client := adminStrategyProcessClient(ctx, t, ca, observer, rpcsecurity.ServiceAPI)
	session := &strategyv1.AdminSession{Token: login.Token}
	page, err := client.ListStrategies(ctx, &strategyv1.ListStrategiesRequest{
		Session: session,
		Limit:   domain.MaxAdminPageSize,
	})
	if err != nil || len(page.Strategies) != len(instances) || page.NextAfterId != 0 {
		t.Fatal("native management query did not return real persisted instances", err)
	}
	for _, instance := range instances {
		detail, err := client.GetStrategy(ctx, &strategyv1.GetStrategyRequest{
			Session:    session,
			StrategyId: instance.Id,
		})
		if err != nil || detail.Strategy.Id != instance.Id || detail.RequestedConfig.StrategyId != instance.Id || detail.RequestedConfig.MarketDataSource != instance.MarketDataSource || detail.ActiveConfig != nil {
			t.Fatal("native typed detail crossed version/source identity", err)
		}
		if domain.StrategyTemplate(instance.Template) == domain.StrategyTemplateGrid && detail.RequestedConfig.Parameters.GetGrid().GetLowerPrice() != "0.123456789012345678" {
			t.Fatal("native protobuf grid decimal lost precision")
		}
		versions, err := client.ListConfigVersions(ctx, &strategyv1.ListConfigVersionsRequest{
			Session:    session,
			StrategyId: instance.Id,
			Limit:      1,
		})
		if err != nil || len(versions.Versions) != 1 || versions.Versions[0].StrategyId != instance.Id {
			t.Fatal("native configuration list crossed instance", err)
		}
	}
	assertAdminStrategyProcessRejectsOtherPeer(ctx, t, ca, observer, session)
	if err := auth.Logout(ctx, login.Token, login.CSRF); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListStrategies(ctx, &strategyv1.ListStrategiesRequest{
		Session: session,
		Limit:   1,
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatal("native management RPC reused a revoked session", err)
	}
	for _, entity := range []any{&modelv1.SimulatorAccount{}, &modelv1.OrderIntent{}, &modelv1.StrategyLease{}, &modelv1.StrategyAction{}} {
		var count int64
		if err := db.WithContext(ctx).Model(entity).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("management-only process provisioned trading resources", err)
		}
	}
}

func adminStrategyProcessClient(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, service string) strategyv1.StrategyManagementClient {
	t.Helper()
	cfg := businessRPCConfig(ca.Issue(t, service, false))
	cfg.AllowedServices = []string{rpcsecurity.ServiceStrategy}
	factory, err := rpcclient.New(ctx, cfg, service, discovery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	connection, err := factory.Client(rpcclient.Target{Service: rpcsecurity.ServiceStrategy})
	if err != nil {
		t.Fatal(err)
	}
	return strategyv1.NewStrategyManagementClient(connection)
}

func assertAdminStrategyProcessRejectsOtherPeer(ctx context.Context, t *testing.T, ca *testcert.Authority, discovery *nacos.Client, session *strategyv1.AdminSession) {
	t.Helper()
	client := adminStrategyProcessClient(ctx, t, ca, discovery, rpcsecurity.ServicePortfolio)
	if _, err := client.ListStrategies(ctx, &strategyv1.ListStrategiesRequest{
		Session: session,
		Limit:   1,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatal("verified non-API peer reached management", err)
	}
}
