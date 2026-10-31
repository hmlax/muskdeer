//go:build integration

package migrate

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/nacos"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/serviceoverview"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestAdminServicesObserveActualNamedProcessesReadinessAndFreshRemovalWithoutTradingWrites(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	_, _, submit := adminCreationFixture(ctx, t, db)
	viewer := riskActorFixture(ctx, t, db, "services-viewer", domain.AdminRoleViewer, 101, domain.AdminSessionTTL)
	before := publicationInstance(ctx, t, db, submit.Input.Authority.StrategyID)
	riskBefore := riskEventSourceRow(ctx, t, db, submit.Input.Authority.StrategyID)
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
	a.InstanceId, a.Version = "service-observer-a", "declared-example-a"
	a.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	startBusinessProcess(ctx, t, a, discovery)
	b := businessProcessConfig(t, ca, postgres, nil, naming, string(domain.AdminServiceStrategy))
	b.InstanceId, b.Version = "service-observer-b", "declared-example-b"
	b.Business.Strategy = &configv1.StrategyRuntime{ManagementEnabled: true}
	bPID := startBusinessProcess(ctx, t, b, discovery)
	api, cfg, blocked := serviceObserverAPI(ctx, t, ca, postgres, naming)
	ownedDB, err := api.DB()
	if err != nil {
		t.Fatal(err)
	}
	auth, err := repository.NewAdminAuth(ownedDB)
	if err != nil {
		t.Fatal(err)
	}
	ownedDiscovery, err := api.Discovery()
	if err != nil {
		t.Fatal(err)
	}
	clients, err := api.GRPCClients()
	if err != nil {
		t.Fatal(err)
	}
	local := serviceoverview.LocalAPI{
		InstanceID: cfg.InstanceId,
		Version:    cfg.Version,
		CheckReady: api.CheckReady,
	}
	reader, err := serviceoverview.New(auth, ownedDiscovery, clients, local, serviceoverview.Settings{AllowedServices: cfg.GrpcClients.AllowedServices})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeServicePages(ctx, t, reader, viewer.Token, a, b)
	assertNativeLocalServiceReadiness(ctx, t, reader, viewer.Token, api, blocked)
	held, err := ownedDiscovery.GetService(ctx, rpcsecurity.ServiceStrategy)
	if err != nil || len(held) != 2 {
		t.Fatal("native fixture did not retain two earlier membership records", err)
	}
	if err := syscall.Kill(bPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitServiceObserverRemoval(ctx, t, ownedDiscovery, b.InstanceId)
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	current, err := reader.Observe(ctx, viewer.Token, filter)
	if err != nil || current.Total != 1 || len(current.Items) != 1 || current.Items[0].InstanceID != a.InstanceId || current.Items[0].Status != domain.AdminServiceReady || current.NextAfterInstanceID != "" {
		t.Fatal("observer cached removed membership or lost the independent live provider", err)
	}
	stale, err := serviceoverview.New(auth, heldServiceInventory{instances: held}, clients, local, serviceoverview.Settings{AllowedServices: cfg.GrpcClients.AllowedServices})
	if err != nil {
		t.Fatal(err)
	}
	filter.AfterInstanceID = a.InstanceId
	missing, err := stale.Observe(ctx, viewer.Token, filter)
	if err != nil || len(missing.Items) != 1 || missing.Items[0].InstanceID != b.InstanceId || missing.Items[0].Status != domain.AdminServiceProbeFailed {
		t.Fatal("required removed instance silently fell back to a live peer", err)
	}
	if !proto.Equal(before, publicationInstance(ctx, t, db, before.Id)) || !proto.Equal(riskBefore, riskEventSourceRow(ctx, t, db, before.Id)) {
		t.Fatal("service observations changed strategy or risk")
	}
	core, _ := executionFixture(t, db)
	assertCoreBalance(ctx, t, core, submit.Input.AccountID, "USDT", "1000", "0")
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
	assertGatewayOrderCount(ctx, t, db, 0)
	assertPortfolioRowCount(ctx, t, db, &modelv1.SimulatorFill{}, 0)
	if err := auth.Logout(ctx, viewer.Token, viewer.CSRF); err != nil {
		t.Fatal(err)
	}
	if value, err := reader.Observe(ctx, viewer.Token, filter); !errors.Is(err, domain.ErrUnauthenticated) || value.Items != nil || value.Total != 0 {
		t.Fatal("actual revoked viewer retained service diagnostics", err)
	}
}

func assertNativeServicePages(ctx context.Context, t *testing.T, reader domain.AdminServices, token string, a, b *configv1.ServiceConfig) {
	t.Helper()
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceStrategy,
		Limit: 1,
	}
	first, err := reader.Observe(ctx, token, filter)
	if err != nil || first.Total != 2 || len(first.Items) != 1 || first.Items[0].InstanceID != a.InstanceId || first.Items[0].DeclaredVersion != a.Version ||
		first.Items[0].Status != domain.AdminServiceReady || first.Items[0].ProbedAt == nil || first.NextAfterInstanceID != a.InstanceId {
		t.Fatal("actual strategy mTLS readiness lost named first provider", err)
	}
	filter.AfterInstanceID = first.NextAfterInstanceID
	last, err := reader.Observe(ctx, token, filter)
	if err != nil || last.Total != 2 || len(last.Items) != 1 || last.Items[0].InstanceID != b.InstanceId || last.Items[0].DeclaredVersion != b.Version || last.Items[0].Status != domain.AdminServiceReady || last.NextAfterInstanceID != "" {
		t.Fatal("actual second provider was replaced by stateless routing", err)
	}
}

type heldServiceInventory struct{ instances []nacos.Instance }

func (h heldServiceInventory) GetService(context.Context, string) ([]nacos.Instance, error) {
	return slices.Clone(h.instances), nil
}

func serviceObserverAPI(ctx context.Context, t *testing.T, ca *testcert.Authority, postgres *configv1.Postgres, naming *configv1.Nacos) (*deer.DeerApi, *configv1.ServiceConfig, *atomic.Bool) {
	t.Helper()
	clients := businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceAPI, false))
	clients.AllowedServices = []string{rpcsecurity.ServiceStrategy}
	cfg := &configv1.ServiceConfig{
		Name:            string(domain.AdminServiceAPI),
		InstanceId:      "service-observer-api",
		Version:         "declared-api-example",
		StartupTimeout:  durationpb.New(5 * time.Second),
		ShutdownTimeout: durationpb.New(5 * time.Second),
		Http: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		Postgres:    proto.CloneOf(postgres),
		Nacos:       proto.CloneOf(naming),
		GrpcClients: clients,
	}
	blocked := &atomic.Bool{}
	api, cleanup, err := deer.NewDeerApi(ctx, cfg, deer.APISpec{Service: deer.ServiceSpec{
		LogOutput: io.Discard,
		Workers: func(context.Context, *deer.Deer) ([]deer.Worker, error) {
			return []deer.Worker{{
				Name: "observer-test-readiness",
				Run: func(ctx context.Context) error {
					<-ctx.Done()
					return nil
				},
				Check: func(context.Context) error {
					if blocked.Load() {
						return errors.New("test readiness dependency is unavailable")
					}
					return nil
				},
			}}, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	run, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), 35*time.Second)
	go func() { done <- api.Run(run) }()
	t.Cleanup(func() {
		defer cancelRun()
		stop, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		if err := cleanup(stop); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-stop.Done():
			t.Error("owned observer API did not finish within its shutdown budget")
		}
	})
	ready, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := api.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	return api, cfg, blocked
}

func assertNativeLocalServiceReadiness(ctx context.Context, t *testing.T, reader domain.AdminServices, token string, api *deer.DeerApi, blocked *atomic.Bool) {
	t.Helper()
	filter := domain.AdminServiceFilter{
		Role:  domain.AdminServiceAPI,
		Limit: 1,
	}
	ready, err := reader.Observe(ctx, token, filter)
	if err != nil || ready.Source != domain.AdminServiceLocalAPI || ready.Total != 1 || len(ready.Items) != 1 || ready.Items[0].Status != domain.AdminServiceReady {
		t.Fatal("current API readiness was inferred from registry membership", err)
	}
	blocked.Store(true)
	defer blocked.Store(false)
	unready, err := reader.Observe(ctx, token, filter)
	if err != nil || len(unready.Items) != 1 || unready.Items[0].Status != domain.AdminServiceNotReady || !api.Ready() {
		t.Fatal("local observer used lifecycle Ready instead of actual CheckReady", err)
	}
}

func waitServiceObserverRemoval(ctx context.Context, t *testing.T, discovery *nacos.Client, id string) {
	t.Helper()
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		instances, err := discovery.GetService(bounded, rpcsecurity.ServiceStrategy)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(instances, func(item nacos.Instance) bool { return item.ID == id }) {
			return
		}
		select {
		case <-bounded.Done():
			t.Fatal("terminated provider did not leave scoped discovery")
		case <-ticker.C:
		}
	}
}
