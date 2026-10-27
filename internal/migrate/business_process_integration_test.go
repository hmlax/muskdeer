//go:build integration

package migrate

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsjs "github.com/nats-io/nats.go/jetstream"
	"github.com/shopspring/decimal"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	executionv1 "github.com/hmlax/muskdeer/api/execution/v1"
	"github.com/hmlax/muskdeer/deer/nacos"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
	"github.com/hmlax/muskdeer/deer/rpcclient"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
	"github.com/hmlax/muskdeer/internal/outbox"
	"github.com/hmlax/muskdeer/internal/repository"
	"github.com/hmlax/muskdeer/internal/testcert"
)

func TestIndependentBusinessProcessesPublishConsumeAndReconcile(t *testing.T) {
	db, postgres := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	_, submit := admissionFixture(ctx, t, db)
	core, _ := executionFixture(t, db)
	portfolio := portfolioFixture(ctx, t, db, submit)
	leases, err := repository.NewLeases(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.Release(ctx, submit.ExecutionAuthority); err != nil {
		t.Fatal(err)
	}
	ca := testcert.New(t)
	stream := businessStream(ctx, t)
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
	executionCfg := businessProcessConfig(t, ca, postgres, stream, naming, "execution")
	executionPID := startBusinessProcess(ctx, t, executionCfg, observer)
	portfolioCfg := businessProcessConfig(t, ca, postgres, stream, naming, "portfolio")
	portfolioPID := startBusinessProcess(ctx, t, portfolioCfg, observer)
	if executionPID == portfolioPID || executionPID == os.Getpid() || portfolioPID == os.Getpid() {
		t.Fatal("business services did not have independent processes")
	}
	factory, err := rpcclient.New(ctx, businessRPCConfig(ca.Issue(t, rpcsecurity.ServiceStrategy, false)), rpcsecurity.ServiceStrategy, observer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := factory.Close(); err != nil {
			t.Error(err)
		}
	})
	connection, err := factory.Client(rpcclient.Target{
		Service:    rpcsecurity.ServiceExecution,
		InstanceID: executionCfg.InstanceId,
	})
	if err != nil {
		t.Fatal(err)
	}
	refreshBusinessRisk(ctx, t, db)
	client := executionv1.NewExecutionClient(connection)
	buy, err := client.SubmitIntent(ctx, rpcSubmitFixture(submit, "process-buy", commonv1.OrderSide_ORDER_SIDE_BUY, "99"))
	if err != nil || buy.Order.State != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatalf("process buy = %v, %v", buy, err)
	}
	sell, err := client.SubmitIntent(ctx, rpcSubmitFixture(submit, "process-sell", commonv1.OrderSide_ORDER_SIDE_SELL, "101"))
	if err != nil || sell.Order.State != commonv1.OrderState_ORDER_STATE_LIVE {
		t.Fatalf("process sell = %v, %v", sell, err)
	}
	for _, order := range []*executionv1.OrderView{buy.Order, sell.Order} {
		price, err := decimal.NewFromString(order.Price)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := core.Fill(ctx, domain.CoreFillRequest{
			AccountID:           submit.Input.AccountID,
			ClientOrderID:       order.ClientOrderId,
			TradeID:             "process-fill-" + order.ClientOrderId,
			CounterpartySubject: "external-liquidity",
			Price:               price,
			Quantity:            decimal.NewFromInt(1),
		}); err != nil {
			t.Fatal(err)
		}
	}
	waitBusinessProjection(ctx, t, db, portfolio, submit.Input.AccountID)
	assertPortfolioRowCount(ctx, t, db, &modelv1.PortfolioFill{}, 2)
	assertPortfolioRowCount(ctx, t, db, &modelv1.ConsumerInbox{}, 2)
	assertReservedBudget(ctx, t, db, submit.Input.AccountID, "0")
}

func waitBusinessProjection(ctx context.Context, t *testing.T, db *gorm.DB, portfolio domain.PortfolioRepository, account string) {
	t.Helper()
	proof, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		projection, err := portfolio.Get(proof, account)
		if err != nil {
			t.Fatal(err)
		}
		var incomplete int64
		if err := db.WithContext(proof).Model(&modelv1.OrderIntent{}).Where("account_id = ? AND state <> ?", account, domain.OrderStateFilled).Count(&incomplete).Error; err != nil {
			t.Fatal(err)
		}
		var pending int64
		if err := db.WithContext(proof).Model(&modelv1.OutboxEvent{}).Where("state <> ?", domain.OutboxStatePublished).Count(&pending).Error; err != nil {
			t.Fatal(err)
		}
		if projection.Reconciled && incomplete == 0 && pending == 0 {
			if !projection.Values.Quantity.Equal(decimal.NewFromInt(10)) || !projection.Values.CashQuote.Equal(decimal.RequireFromString("1001.8")) || !projection.Values.FeesQuote.Equal(decimal.RequireFromString("0.2")) {
				t.Fatalf("independent processes changed exact business effects: %+v", projection)
			}
			return
		}
		select {
		case <-proof.Done():
			t.Fatalf("business processes did not complete projection/OMS/outbox: reconciled=%v orders=%d pending=%d", projection.Reconciled, incomplete, pending)
		case <-ticks.C:
		}
	}
}

func businessStream(ctx context.Context, t *testing.T) *configv1.JetStream {
	t.Helper()
	name := strings.ToLower(rand.Text())[:16]
	cfg := &configv1.JetStream{
		Address:         os.Getenv("MUSKDEER_TEST_NATS_ADDRESS"),
		TokenEnv:        "MUSKDEER_TEST_NATS_TOKEN",
		Stream:          "MUSKDEER_BUSINESS_" + name,
		SubjectPrefix:   "muskdeer.integration.business_" + name,
		ConnectTimeout:  durationpb.New(time.Second),
		CommandTimeout:  durationpb.New(time.Second),
		MaxInflight:     2,
		MaxMessageBytes: 64 * 1024,
		TlsEnabled:      proto.Bool(false),
	}
	connection, err := nats.Connect("nats://"+cfg.Address, nats.Token(os.Getenv(cfg.TokenEnv)), nats.NoReconnect(), nats.Timeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	admin, err := natsjs.New(connection, natsjs.WithDefaultTimeout(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := admin.CreateStream(ctx, natsjs.StreamConfig{
		Name:       cfg.Stream,
		Subjects:   []string{cfg.SubjectPrefix + ".>"},
		Storage:    natsjs.FileStorage,
		Retention:  natsjs.LimitsPolicy,
		Discard:    natsjs.DiscardNew,
		MaxBytes:   4 * 1024 * 1024,
		MaxMsgs:    1000,
		MaxMsgSize: 64 * 1024,
		MaxAge:     10 * time.Minute,
		Duplicates: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.CreateConsumer(ctx, natsjs.ConsumerConfig{
		Durable:       "portfolio_business",
		FilterSubject: cfg.SubjectPrefix + "." + outbox.ExecutionSubject,
		AckPolicy:     natsjs.AckExplicitPolicy,
		AckWait:       10 * time.Second,
		MaxDeliver:    5,
		MaxAckPending: 4,
		MaxWaiting:    4,
	}); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func businessNaming() *configv1.Nacos {
	return &configv1.Nacos{
		Address:     os.Getenv("MUSKDEER_TEST_NACOS_ADDRESS"),
		NamespaceId: "integration-naming",
		// infrastructureprobe runs concurrently with its own test CA and
		// market.grpc processes. Never discover those unrelated fixtures.
		Group:          "MUSKDEER_BUSINESS_TEST",
		Cluster:        "TEST",
		Username:       "nacos",
		PasswordEnv:    "MUSKDEER_TEST_NACOS_PASSWORD",
		ConnectTimeout: durationpb.New(2 * time.Second),
		CommandTimeout: durationpb.New(time.Second),
		MaxInflight:    4,
		TlsEnabled:     proto.Bool(false),
	}
}

func businessRPCConfig(identity *configv1.MutualTLS) *configv1.GRPCClients {
	return &configv1.GRPCClients{
		Mtls:             identity,
		ConnectTimeout:   durationpb.New(time.Second),
		RequestTimeout:   durationpb.New(2 * time.Second),
		MaxInflight:      4,
		MaxRequestBytes:  64 * 1024,
		MaxResponseBytes: 64 * 1024,
		AllowedServices:  []string{rpcsecurity.ServiceExecution},
	}
}

func businessProcessConfig(t *testing.T, ca *testcert.Authority, postgres *configv1.Postgres, stream *configv1.JetStream, naming *configv1.Nacos, role string) *configv1.ServiceConfig {
	t.Helper()
	cfg := &configv1.ServiceConfig{
		Name:            role,
		InstanceId:      "business-" + role,
		Version:         "test",
		StartupTimeout:  durationpb.New(5 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(3 * time.Second),
		},
		Monitoring: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
		Postgres:  proto.CloneOf(postgres),
		Jetstream: proto.CloneOf(stream),
		Nacos:     proto.CloneOf(naming),
		GrpcSecurity: &configv1.GRPCServerSecurity{
			Mtls:                ca.Issue(t, role+".grpc", false),
			AllowedPeerServices: []string{rpcsecurity.ServiceStrategy, rpcsecurity.ServicePortfolio, rpcsecurity.ServiceAPI},
		},
		Business: &configv1.BusinessRuntime{
			Enabled:          true,
			ExecutionVenue:   domain.ExecutionVenueSimulator,
			PollInterval:     durationpb.New(20 * time.Millisecond),
			OperationTimeout: durationpb.New(3 * time.Second),
		},
	}
	cfg.Nacos.Registration = &configv1.NacosRegistration{
		AdvertiseIp:       "127.0.0.1",
		HeartbeatInterval: durationpb.New(time.Second),
	}
	if role+".grpc" == rpcsecurity.ServicePortfolio {
		cfg.Business.PortfolioConsumer = "portfolio_business"
		cfg.GrpcClients = businessRPCConfig(ca.Issue(t, rpcsecurity.ServicePortfolio, false))
	}
	return cfg
}

func startBusinessProcess(ctx context.Context, t *testing.T, cfg *configv1.ServiceConfig, observer *nacos.Client) int {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "business.json")
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(directory, "output.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	run, cancelRun := context.WithTimeout(context.WithoutCancel(t.Context()), 40*time.Second)
	command := exec.CommandContext(run, filepath.Join("..", "..", ".cache", "bin", "muskdeer"), "-role", cfg.Name, "-config", configPath)
	command.Stdout, command.Stderr = output, output
	finished := make(chan error, 1)
	t.Cleanup(func() {
		defer cancelRun()
		if command.Process != nil {
			if err := command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Error(err)
			}
			if err := <-finished; err != nil {
				t.Errorf("business process %s exited: %v", cfg.Name, err)
			}
		}
		if err := output.Close(); err != nil {
			t.Error(err)
		}
		if t.Failed() {
			if data, err := os.ReadFile(output.Name()); err == nil {
				t.Logf("business process %s diagnostic: %s", cfg.Name, data)
			}
		}
	})
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { finished <- command.Wait() }()
	ready, cancelReady := context.WithTimeout(ctx, 5*time.Second)
	defer cancelReady()
	ticks := time.NewTicker(20 * time.Millisecond)
	defer ticks.Stop()
	for {
		instances, err := observer.GetService(ready, cfg.Name+".grpc")
		if err != nil {
			t.Fatal(err)
		}
		for _, instance := range instances {
			if instance.ID == cfg.InstanceId {
				return command.Process.Pid
			}
		}
		select {
		case <-ready.Done():
			t.Fatal("business process did not publish within startup budget")
		case <-ticks.C:
		}
	}
}

func refreshBusinessRisk(ctx context.Context, t *testing.T, db *gorm.DB) {
	t.Helper()
	var clock struct{ NowMS int64 }
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).
		Select("floor(extract(epoch FROM clock_timestamp()) * 1000)::bigint AS now_ms").Take(&clock).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.RiskSnapshot{}).Where("delete_at IS NULL").Select("ObservedAt", "ExpiresAt", "Revision").Updates(&modelv1.RiskSnapshot{
		ObservedAt: clock.NowMS,
		ExpiresAt:  clock.NowMS + domain.MaxRiskSnapshotAge.Milliseconds(),
		Revision:   2,
	}).Error; err != nil {
		t.Fatal(err)
	}
}
