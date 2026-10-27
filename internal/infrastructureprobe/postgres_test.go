//go:build integration

package infrastructureprobe

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm/clause"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestPostgresInfrastructure(t *testing.T) {
	if os.Getenv("MUSKDEER_TEST_POSTGRES_PASSWORD") == "" {
		t.Fatal("isolated PostgreSQL credentials are required; run make integration")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	client := openClient(t, postgresConfig(t), "fixture")
	pool, err := client.GORM().DB()
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("testdata/001_storage_probe.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Reviewed fixture DDL is the only raw SQL in this probe.
	if _, err := pool.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatal(err)
	}
	t.Run("generated model round trip", testGeneratedModel)
	t.Run("bounded pool and cancellation", testPoolLimit)
	t.Run("server statement timeout", testStatementTimeout)
	t.Run("deer dependencies and readiness", testDeerDependencies)
	t.Run("API failure closes owned pool", testAPIRollback)
	t.Run("authentication failure is safe and typed", testAuthenticationFailure)
}

func testAuthenticationFailure(t *testing.T) {
	const invalidPassword = "invalid_test_only_password"
	const secretReference = "MUSKDEER_TEST_INVALID_POSTGRES_PASSWORD"
	t.Setenv(secretReference, invalidPassword)
	cfg := postgresConfig(t)
	cfg.PasswordEnv = secretReference
	client, err := postgres.New(t.Context(), cfg, "authentication-rejection")
	var serverError *pgconn.PgError
	if client != nil || !errors.As(err, &serverError) || serverError.Code != "28P01" {
		t.Fatalf("authentication rejection = %v, %v; want SQLSTATE 28P01", client, err)
	}
	if strings.Contains(err.Error(), invalidPassword) {
		t.Fatalf("password leaked in authentication failure: %v", err)
	}
}

func testGeneratedModel(t *testing.T) {
	client := openClient(t, postgresConfig(t), "model-roundtrip")
	row := &modelv1.StorageProbe{
		Venue:      "binance",
		Instrument: "p02-roundtrip",
		Price:      "12345678901234567890.123456789012345678",
		Enabled:    true,
		Revision:   1,
	}
	if err := client.GORM().WithContext(t.Context()).Model(&modelv1.StorageProbe{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	var stored modelv1.StorageProbe
	if err := client.GORM().WithContext(t.Context()).Model(&modelv1.StorageProbe{}).First(&stored, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Id <= 0 || stored.CreateAt == 0 || stored.UpdateAt == 0 || stored.DeleteAt != nil || stored.Price != row.Price {
		t.Fatalf("generated model round trip = %v", &stored)
	}
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Ping(t.Context()); err == nil {
		t.Fatal("closed pool was reopened by health check")
	}
}

func testPoolLimit(t *testing.T) {
	cfg := postgresConfig(t)
	cfg.MaxOpenConns = 1
	client := openClient(t, cfg, "pool-limit")
	pool, err := client.GORM().DB()
	if err != nil {
		t.Fatal(err)
	}
	held, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := held.Close(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := client.Ping(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted pool Ping = %v; want deadline", err)
	}
	if stats := client.Stats(); stats.MaxOpenConnections != 1 || stats.WaitCount == 0 {
		t.Fatalf("bounded pool stats = %+v", stats)
	}
}

func testStatementTimeout(t *testing.T) {
	cfg := postgresConfig(t)
	cfg.StatementTimeout = durationpb.New(250 * time.Millisecond)
	client := openClient(t, cfg, "statement-timeout")
	db := client.GORM()
	row := &modelv1.StorageProbe{
		Venue:      "okx",
		Instrument: "p02-lock-timeout",
		Price:      "0.1",
		Revision:   1,
	}
	if err := db.WithContext(t.Context()).Model(&modelv1.StorageProbe{}).Create(row).Error; err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(t.Context()).Model(&modelv1.StorageProbe{}).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
	}()
	var locked modelv1.StorageProbe
	if err := tx.WithContext(t.Context()).Model(&modelv1.StorageProbe{}).Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	err := db.WithContext(t.Context()).Model(&modelv1.StorageProbe{}).Where("id = ?", row.Id).Update("price", "0.2").Error
	var serverError *pgconn.PgError
	if !errors.As(err, &serverError) || serverError.Code != "57014" {
		t.Fatalf("blocked statement = %v; want server statement_timeout", err)
	}
	var stored modelv1.StorageProbe
	if err := db.WithContext(t.Context()).Model(&modelv1.StorageProbe{}).First(&stored, row.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(stored.Price).Equal(decimal.RequireFromString(row.Price)) {
		t.Fatal("timed-out statement changed the row")
	}
}

func testDeerDependencies(t *testing.T) {
	cfg := serviceConfig(t, "dependency-ready")
	var registered bool
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(ctx context.Context, owner *deer.Deer, _ grpc.ServiceRegistrar) error {
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("registration context has no startup deadline")
			}
			db, err := owner.DB()
			if err != nil {
				return err
			}
			var count int64
			if err := db.WithContext(ctx).Model(&modelv1.StorageProbe{}).Count(&count).Error; err != nil {
				return err
			}
			registered = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	if !registered || !errors.Is(service.CheckReady(t.Context()), deer.ErrNotReady) {
		t.Fatal("constructor readiness/registration contract failed")
	}
	result := make(chan error, 1)
	go func() { result <- service.Run(t.Context()) }()
	ready, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ready); err != nil {
		t.Fatal(err)
	}
	db, err := service.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	} // Controlled dependency loss.
	if err := service.CheckReady(ready); err == nil {
		t.Fatal("readiness ignored unavailable PostgreSQL")
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func testAPIRollback(t *testing.T) {
	cfg := serviceConfig(t, "api-pool-rollback")
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	failure := errors.New("management route assembly failed")
	var pool *sql.DB
	api, cleanup, err := deer.NewDeerApi(t.Context(), cfg, deer.APISpec{
		Service: deer.ServiceSpec{
			LogOutput: io.Discard,
		},
		Routes: func(_ context.Context, owner *deer.Deer, _ *server.Hertz) error {
			db, err := owner.DB()
			if err != nil {
				return err
			}
			pool, err = db.DB()
			if err != nil {
				return err
			}
			return failure
		},
	})
	if api != nil || cleanup != nil || !errors.Is(err, failure) {
		t.Fatalf("API initialization = %v, cleanup nil %v, %v", api, cleanup == nil, err)
	}
	if pool == nil || pool.PingContext(t.Context()) == nil || pool.Stats().OpenConnections != 0 {
		t.Fatal("API construction failure did not close its shared pool")
	}
}

func openClient(t *testing.T, cfg *configv1.Postgres, name string) *postgres.Client {
	t.Helper()
	client, err := postgres.New(t.Context(), cfg, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func postgresConfig(t *testing.T) *configv1.Postgres {
	t.Helper()
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || port == 0 {
		t.Fatal("isolated PostgreSQL port is required; run make integration")
	}
	return &configv1.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         "muskdeer_p01",
		Username:         "muskdeer",
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           "muskdeer_p02",
		MaxOpenConns:     2,
		MaxIdleConns:     1,
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(30 * time.Second),
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(time.Second),
	}
}

func serviceConfig(t *testing.T, id string) *configv1.ServiceConfig {
	t.Helper()
	return &configv1.ServiceConfig{
		Name:            "infrastructure-probe",
		InstanceId:      id,
		Version:         "test",
		StartupTimeout:  durationpb.New(3 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Grpc: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
		Postgres: proto.CloneOf(postgresConfig(t)),
	}
}
