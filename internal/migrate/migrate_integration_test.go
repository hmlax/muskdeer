//go:build integration

package migrate

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/goleak"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/postgres"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestConcurrentMigrationAndGeneratedLedgerConstraints(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 4)
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			version, err := Apply(ctx, db)
			if err == nil && version != 22 {
				err = fmt.Errorf("migration version = %d", version)
			}
			results <- err
		})
	}
	close(start)
	workers.Wait()
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var rows []*modelv1.SchemaMigration
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 22 || rows[0].Id <= 0 || rows[0].CreateAt <= 0 || rows[0].UpdateAt <= 0 || rows[0].DeleteAt != nil {
		t.Fatalf("generated ledger = %v", rows)
	}
	for _, column := range []string{"create_at", "update_at"} {
		err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("id = ?", rows[0].Id).UpdateColumn(column, nil).Error
		var databaseError *pgconn.PgError
		if !errors.As(err, &databaseError) || databaseError.Code != "23502" {
			t.Fatalf("%s NULL constraint = %v", column, err)
		}
	}
	extra := &modelv1.SchemaMigration{
		Version:  23,
		Name:     "future-test.sql",
		Checksum: strings.Repeat("a", 64),
	}
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Create(extra).Error; err != nil {
		t.Fatal(err)
	}
	if extra.Id <= rows[0].Id || extra.DeleteAt != nil {
		t.Fatal("generated identity did not increment or nullable delete changed")
	}
	if _, err := Apply(ctx, db); !errors.Is(err, ErrHistoryMismatch) {
		t.Fatalf("older binary accepted future history: %v", err)
	}
}

func TestFailedMigrationRollsBackDDLAndRejectsChangedHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	deletedAt := time.Now().UTC().UnixMilli()
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).UpdateColumn("delete_at", deletedAt).Error; err != nil {
		t.Fatal(err)
	}
	var marked modelv1.SchemaMigration
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).First(&marked).Error; err != nil {
		t.Fatal(err)
	}
	if marked.DeleteAt == nil || *marked.DeleteAt != deletedAt {
		t.Fatal("nullable deletion time did not round-trip")
	}
	if _, err := Apply(ctx, db); !errors.Is(err, ErrHistoryMismatch) {
		t.Fatalf("deleted history was accepted: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).UpdateColumn("delete_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	definitions, err := loadDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	const failingDDL = "ALTER TABLE schema_migrations ADD COLUMN rollback_probe boolean; ALTER TABLE missing_migration_test_table ADD COLUMN failed_probe boolean;"
	definitions = append(definitions, definition{
		version:  11,
		name:     "0011_rollback_probe.sql",
		sql:      failingDDL,
		checksum: fmt.Sprintf("%x", sha256.Sum256([]byte(failingDDL))),
	})
	if err := applyDefinitions(ctx, db, definitions); err == nil {
		t.Fatal("invalid migration unexpectedly committed")
	}
	if db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Migrator().HasColumn(&modelv1.SchemaMigration{}, "rollback_probe") {
		t.Fatal("failed version left its earlier DDL committed")
	}
	if version, err := Apply(ctx, db); err != nil || version != 22 {
		t.Fatalf("rollback left a lock or partial history: %d, %v", version, err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).
		Select("Checksum").Updates(&modelv1.SchemaMigration{Checksum: strings.Repeat("0", 64)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db); !errors.Is(err, ErrHistoryMismatch) {
		t.Fatalf("checksum rewrite was accepted: %v", err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version > ?", 0).Delete(&modelv1.SchemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db); !errors.Is(err, ErrHistoryMismatch) {
		t.Fatalf("empty existing ledger was silently re-created: %v", err)
	}
}

func TestMigrationLockContentionHonorsCallerDeadline(t *testing.T) {
	db, _ := migrationDatabase(t)
	holderCtx, cancelHolder := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelHolder()
	locked := make(chan struct{})
	release := make(chan struct{})
	completed := make(chan struct{})
	var holderError error
	go func() {
		defer close(completed)
		holderError = db.WithContext(holderCtx).Model(&modelv1.SchemaMigration{}).Transaction(func(tx *gorm.DB) error {
			if err := tx.WithContext(holderCtx).Model(&modelv1.SchemaMigration{}).
				Exec("SELECT pg_advisory_xact_lock(?, ?)", migrationLockNamespace, migrationLockKey).Error; err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-holderCtx.Done():
				return holderCtx.Err()
			}
		})
	}()
	var once sync.Once
	finish := func() {
		once.Do(func() { close(release) })
		<-completed
	}
	t.Cleanup(finish)
	select {
	case <-locked:
	case <-completed:
		t.Fatalf("could not establish lock contention: %v", holderError)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := Apply(ctx, db); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait ignored caller deadline: %v", err)
	}
	finish()
	if holderError != nil {
		t.Fatal(holderError)
	}
	retry, cancelRetry := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancelRetry()
	if version, err := Apply(retry, db); err != nil || version != 22 {
		t.Fatalf("canceled lock wait left partial state: %d, %v", version, err)
	}
}

func TestMigrationCommandAppliesOnlyItsConfiguredSchema(t *testing.T) {
	_, databaseConfig := migrationDatabase(t)
	cfg := &configv1.ServiceConfig{
		Name:            "cron",
		InstanceId:      "migration-command",
		Version:         "test",
		StartupTimeout:  durationpb.New(3 * time.Second),
		ShutdownTimeout: durationpb.New(3 * time.Second),
		Postgres:        databaseConfig,
		Monitoring: &configv1.Listener{
			Address:        "127.0.0.1:0",
			RequestTimeout: durationpb.New(time.Second),
		},
	}
	data, err := protojson.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "migration.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		command := exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", "migrate"), "-apply", "-config", path)
		output, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("migration command = %v, %s", err, output)
		}
		var reply struct {
			Status  string `json:"status"`
			Version int64  `json:"version"`
		}
		if err := json.Unmarshal(output, &reply); err != nil || reply.Status != "applied" || reply.Version != 22 {
			t.Fatalf("migration command output = %s, %v", output, err)
		}
	}
}

func migrationDatabase(t *testing.T) (*gorm.DB, *configv1.Postgres) {
	t.Helper()
	port, err := strconv.ParseUint(os.Getenv("MUSKDEER_TEST_POSTGRES_PORT"), 10, 16)
	if err != nil || port == 0 || os.Getenv("MUSKDEER_TEST_POSTGRES_PASSWORD") == "" {
		t.Fatal("isolated PostgreSQL environment required; run make integration")
	}
	schema := "migration_" + strings.ToLower(rand.Text())
	cfg := &configv1.Postgres{
		Host:             "127.0.0.1",
		Port:             uint32(port),
		Database:         "muskdeer_p01",
		Username:         "muskdeer",
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
		if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Exec("DROP SCHEMA " + identifier + " CASCADE").Error; err != nil {
			t.Error(err)
		}
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Exec("CREATE SCHEMA " + identifier).Error; err != nil {
		t.Fatal(err)
	}
	return db, cfg
}
