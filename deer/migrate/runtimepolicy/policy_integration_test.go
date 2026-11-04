//go:build integration

package runtimepolicy_test

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/migrate"
	policymigrate "github.com/hmlax/muskdeer/deer/migrate/runtimepolicy"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
	"github.com/hmlax/muskdeer/deer/postgres"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
	"github.com/hmlax/muskdeer/deer/trading/runtimeconfig"
)

func policyDatabase(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
	db, _ := runtimePolicyDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	if _, err := migrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := policymigrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := policymigrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func policyConfiguration(name string, fraction string) *configv1.ServiceConfig {
	return &configv1.ServiceConfig{
		Name: name,
		Business: &configv1.BusinessRuntime{
			ExecutionVenue:   native.Venue,
			PollInterval:     durationpb.New(200 * time.Millisecond),
			OperationTimeout: durationpb.New(time.Second),
			Venue: &configv1.VenueRuntime{
				QuantityFraction: fraction,
				Depth:            20,
				LeaseTtl:         durationpb.New(10 * time.Second),
			},
		},
	}
}

func readPolicy(ctx context.Context, t *testing.T, db *gorm.DB, service string) *model.ServiceRuntimePolicy {
	t.Helper()
	var row model.ServiceRuntimePolicy
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", service).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	return &row
}

func TestRuntimePolicyAuthoritativeSnapshotAndIdempotentImport(t *testing.T) {
	db, ctx := policyDatabase(t)
	input := policyConfiguration("strategy", "0.123456789012345678")
	if _, err := runtimeconfig.LoadDatabasePolicy(ctx, db, input); !errors.Is(err, runtimeconfig.ErrPolicyMissing) {
		t.Fatalf("missing row fell back to supplied policy: %v", err)
	}
	rows, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("import: %v", err)
	}
	before := proto.CloneOf(rows[0])
	if before.Id <= 0 || before.CreateAt <= 0 || before.UpdateAt <= 0 || before.DeleteAt != nil || before.Revision != 1 {
		t.Fatal("generated model lost identity/timestamp/revision semantics")
	}
	rows, err = runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input}})
	if err != nil || len(rows) != 1 || !proto.Equal(before, rows[0]) {
		t.Fatalf("idempotent import wrote a new revision: %v", err)
	}
	input.Business.Venue.QuantityFraction = "0.9"
	loaded, err := runtimeconfig.LoadDatabasePolicy(ctx, db, input)
	if err != nil || loaded.Business.Venue.QuantityFraction != "0.123456789012345678" || input.Business.Venue.QuantityFraction != "0.9" {
		t.Fatalf("database snapshot was overridden or caller mutated: %v", err)
	}
	loaded.Business.Venue.Depth = 1
	again, err := runtimeconfig.LoadDatabasePolicy(ctx, db, input)
	if err != nil || again.Business.Venue.Depth != 20 {
		t.Fatal("borrowed mutable policy escaped")
	}
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("id = ?", before.Id).Update("parameters_digest", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.LoadDatabasePolicy(ctx, db, input); err == nil {
		t.Fatal("changed stored parameters were accepted")
	}
}

func TestRuntimePolicyConcurrentRevisionAndAtomicBatchRollback(t *testing.T) {
	db, ctx := policyDatabase(t)
	a, b := policyConfiguration("market", "0.1"), policyConfiguration("strategy", "0.1")
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: a}, {Config: b}}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	var group sync.WaitGroup
	for _, fraction := range []string{"0.2", "0.3"} {
		changed := proto.CloneOf(a)
		changed.Business.Venue.QuantityFraction = fraction
		group.Go(func() {
			<-start
			_, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: changed, ExpectedRevision: 1}})
			outcomes <- err
		})
	}
	close(start)
	group.Wait()
	close(outcomes)
	succeeded, conflicted := 0, 0
	for err := range outcomes {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, runtimeconfig.ErrPolicyConflict):
			conflicted++
		default:
			t.Fatal(err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("revision admission = %d/%d", succeeded, conflicted)
	}
	beforeA, beforeB := readPolicy(ctx, t, db, "market"), readPolicy(ctx, t, db, "strategy")
	a.Business.Venue.Depth = 5
	b.Business.Venue.Depth = 5
	_, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{
		{Config: a, ExpectedRevision: 2},
		{Config: b, ExpectedRevision: 99},
	})
	if !errors.Is(err, runtimeconfig.ErrPolicyConflict) {
		t.Fatalf("stale batch = %v", err)
	}
	if !proto.Equal(beforeA, readPolicy(ctx, t, db, "market")) || !proto.Equal(beforeB, readPolicy(ctx, t, db, "strategy")) {
		t.Fatal("failed batch partially changed policies")
	}
}

func TestRuntimePolicyRejectsActiveOwnershipAndInvalidInput(t *testing.T) {
	db, ctx := policyDatabase(t)
	input := policyConfiguration("strategy", "0.1")
	invalid := proto.CloneOf(input)
	invalid.Business.ExecutionVenue = "unsupported"
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: invalid}}); err == nil {
		t.Fatal("unsupported route imported")
	}
	if err := db.WithContext(ctx).Model(&model.ExecutionShardLease{}).Create(&model.ExecutionShardLease{
		AccountId: "policy-guard",
		OwnerId:   "policy-owner",
		Epoch:     1,
		ExpiresAt: time.Now().Add(time.Minute).UnixMilli(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input}}); !errors.Is(err, domain.ErrLeaseHeld) {
		t.Fatalf("active owner was not protected: %v", err)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("rejected import left policy rows")
	}
}

func runtimePolicyDatabase(t *testing.T) (*gorm.DB, *configv1.Postgres) {
	t.Helper()
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || port == 0 || os.Getenv("MUSKDEER_TEST_POSTGRES_PASSWORD") == "" {
		t.Fatal("isolated PostgreSQL environment required; run make integration")
	}
	schema := "migration_" + strings.ToLower(rand.Text())
	cfg := &configv1.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         os.Getenv("MUSKDEER_TEST_POSTGRES_DATABASE"),
		Username:         os.Getenv("MUSKDEER_TEST_POSTGRES_USERNAME"),
		PasswordEnv:      "MUSKDEER_TEST_POSTGRES_PASSWORD",
		SslMode:          postgres.SSLModeDisable,
		Schema:           schema,
		MaxOpenConns:     4,
		MaxIdleConns:     2,
		MaxLifetime:      durationpb.New(time.Minute),
		MaxIdleTime:      durationpb.New(30 * time.Second),
		ConnectTimeout:   durationpb.New(time.Second),
		StatementTimeout: durationpb.New(2 * time.Second),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	client, err := postgres.New(ctx, cfg, "migration-test")
	if err != nil {
		t.Fatal(err)
	}
	db := client.GORM()
	// Only reviewed fixture DDL uses an identifier derived from this test's
	// random schema; business table selection always binds a generated Model.
	identifier := pgx.Identifier{schema}.Sanitize()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 3*time.Second)
		defer cancel()
		if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec("DROP SCHEMA " + identifier + " CASCADE").Error; err != nil {
			t.Error(err)
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec("CREATE SCHEMA " + identifier).Error; err != nil {
		t.Fatal(err)
	}
	return db, cfg
}

func TestRuntimePolicyEnablementAndDeletionAreExplicit(t *testing.T) {
	db, ctx := policyDatabase(t)
	input := policyConfiguration("market", "0.1")
	input.ShutdownTimeout = durationpb.New(5 * time.Second)
	input.Postgres = &configv1.Postgres{}
	input.Grpc = &configv1.Listener{}
	input.GrpcSecurity = &configv1.GRPCServerSecurity{Mtls: &configv1.MutualTLS{}}
	input.Nacos = &configv1.Nacos{Registration: &configv1.NacosRegistration{}}
	input.Business.Enabled = true
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input}}); err != nil {
		t.Fatal(err)
	}
	input.Business.Enabled = false
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input, ExpectedRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	supplied := proto.CloneOf(input)
	supplied.Business.Enabled = true
	loaded, err := runtimeconfig.LoadDatabasePolicy(ctx, db, supplied)
	if err != nil || loaded.Business.Enabled {
		t.Fatalf("explicit false lost: %v", err)
	}
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Where("service_name = ?", input.Name).Update("delete_at", time.Now().UnixMilli()).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.LoadDatabasePolicy(ctx, db, supplied); !errors.Is(err, runtimeconfig.ErrPolicyMissing) {
		t.Fatalf("deleted policy fell back: %v", err)
	}
	if _, err := runtimeconfig.ImportDatabasePolicies(ctx, db, []runtimeconfig.PolicyWrite{{Config: input, ExpectedRevision: 2}}); err == nil {
		t.Fatal("deleted policy silently recreated")
	}
}

func TestRuntimePolicyMigrationConflictRollsBackWholeSchema(t *testing.T) {
	db, _ := runtimePolicyDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Exec("CREATE TABLE service_runtime_policies (id bigint PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	if err := policymigrate.Apply(ctx, db); err == nil {
		t.Fatal("conflicting schema accepted")
	}
	if db.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Migrator().HasTable(&model.RuntimePolicyMigration{}) {
		t.Fatal("failed DDL left migration ledger")
	}
	if err := db.WithContext(ctx).Model(&model.ServiceRuntimePolicy{}).Exec("DROP TABLE service_runtime_policies").Error; err != nil {
		t.Fatal(err)
	}
	if err := policymigrate.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&model.RuntimePolicyMigration{}).Where("version = ?", 1).Update("checksum", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := policymigrate.Check(ctx, db); !errors.Is(err, policymigrate.ErrHistory) {
		t.Fatalf("changed migration accepted: %v", err)
	}
}
