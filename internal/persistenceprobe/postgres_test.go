//go:build integration

package persistenceprobe

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

const probeVenue = "binance"

func TestPostgresContract(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dsn := os.Getenv("MUSKDEER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("MUSKDEER_TEST_POSTGRES_DSN is required; run make integration")
	}
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open isolated PostgreSQL = %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close isolated PostgreSQL = %v", err)
		}
	})
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(2)
	pool.SetConnMaxLifetime(time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated PostgreSQL = %v", err)
	}
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open(isolated PostgreSQL) = %v", err)
	}
	fixture, err := os.ReadFile("testdata/001_storage_probe.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.ExecContext(ctx, string(fixture)); err != nil {
		t.Fatalf("apply P01 fixture DDL = %v", err)
	}
	db = db.WithContext(ctx)

	t.Run("database constraints", func(t *testing.T) {
		testDatabaseConstraints(t, db)
	})

	t.Run("identity numeric and unique key", func(t *testing.T) {
		testIdentityNumericAndUniqueKey(t, db)
	})

	t.Run("timestamps and zero value updates", func(t *testing.T) {
		testTimestampsAndZeroValueUpdates(t, db)
	})

	t.Run("nullable deletion time", func(t *testing.T) {
		testNullableDeletionTime(t, db)
	})

	t.Run("transaction rollback", func(t *testing.T) {
		testTransactionRollback(t, db)
	})
}

func testDatabaseConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(&modelv1.StorageProbe{})
	if err != nil {
		t.Fatalf("ColumnTypes(StorageProbe) = %v", err)
	}
	if len(columns) != 9 {
		t.Errorf("ColumnTypes(StorageProbe) count = %d, want 9", len(columns))
	}
	for _, column := range columns {
		switch column.Name() {
		case "id", "create_at", "update_at":
			if nullable, known := column.Nullable(); !known || nullable {
				t.Errorf("%s.Nullable() = %v, %v; want false, true", column.Name(), nullable, known)
			}
		case "delete_at":
			if nullable, known := column.Nullable(); !known || !nullable {
				t.Errorf("delete_at.Nullable() = %v, %v; want true, true", nullable, known)
			}
		case "price":
			precision, scale, known := column.DecimalSize()
			if !known || precision != 38 || scale != 18 {
				t.Errorf("price.DecimalSize() = %d, %d, %v; want 38, 18, true", precision, scale, known)
			}
		}
	}
}

func testIdentityNumericAndUniqueKey(t *testing.T, db *gorm.DB) {
	t.Helper()
	first := newProbe("identity-1")
	second := newProbe("identity-2")
	for _, record := range []*modelv1.StorageProbe{first, second} {
		if err := db.Model(&modelv1.StorageProbe{}).Create(record).Error; err != nil {
			t.Fatalf("Create(StorageProbe) = %v", err)
		}
	}
	if first.Id <= 0 || second.Id <= first.Id {
		t.Errorf("database generated IDs = %d, %d; want positive increasing IDs", first.Id, second.Id)
	}
	var stored modelv1.StorageProbe
	if err := db.Model(&modelv1.StorageProbe{}).First(&stored, first.Id).Error; err != nil {
		t.Fatal(err)
	}
	price, err := decimal.NewFromString(stored.Price)
	if err != nil || price.String() != precisePrice {
		t.Errorf("numeric round trip = %q, %v; want %q", stored.Price, err, precisePrice)
	}
	duplicate := newProbe("identity-1")
	requireSQLState(t, db.Model(&modelv1.StorageProbe{}).Create(duplicate).Error, "23505")
	duplicateID := newProbe("identity-3")
	duplicateID.Id = first.Id
	requireSQLState(t, db.Model(&modelv1.StorageProbe{}).Create(duplicateID).Error, "23505")
}

func testTimestampsAndZeroValueUpdates(t *testing.T, db *gorm.DB) {
	t.Helper()
	created := time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)
	updated := created.Add(time.Second)
	record := newProbe("timestamps")
	createDB := db.Session(&gorm.Session{NowFunc: func() time.Time { return created }})
	if err := createDB.Model(&modelv1.StorageProbe{}).Create(record).Error; err != nil {
		t.Fatal(err)
	}
	if record.CreateAt != created.UnixMilli() || record.UpdateAt != created.UnixMilli() {
		t.Errorf("insert timestamps = %d, %d; want %d", record.CreateAt, record.UpdateAt, created.UnixMilli())
	}
	updateDB := db.Session(&gorm.Session{NowFunc: func() time.Time { return updated }})
	if err := updateDB.Model(&modelv1.StorageProbe{}).Where("id = ?", record.Id).
		Select("enabled", "revision").Updates(&modelv1.StorageProbe{}).Error; err != nil {
		t.Fatal(err)
	}
	var stored modelv1.StorageProbe
	if err := db.Model(&modelv1.StorageProbe{}).First(&stored, record.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Enabled || stored.Revision != 0 || stored.CreateAt != created.UnixMilli() || stored.UpdateAt != updated.UnixMilli() {
		t.Errorf("updated record = %v; want false/0, original create time and new update time", &stored)
	}
	for _, column := range []string{"create_at", "update_at"} {
		// Check the database constraint without GORM's automatic timestamp callback.
		requireSQLState(t, db.Model(&modelv1.StorageProbe{}).Where("id = ?", record.Id).UpdateColumn(column, nil).Error, "23502")
	}
}

func testNullableDeletionTime(t *testing.T, db *gorm.DB) {
	t.Helper()
	record := newProbe("nullable")
	if err := db.Model(&modelv1.StorageProbe{}).Create(record).Error; err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&modelv1.StorageProbe{}).Where("id = ? AND delete_at IS NULL", record.Id).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("SQL NULL deletion time count = %d, want 1", count)
	}
	for _, value := range []int64{0, time.Date(2026, time.October, 26, 1, 0, 0, 0, time.UTC).UnixMilli()} {
		if err := db.Model(&modelv1.StorageProbe{}).Where("id = ?", record.Id).
			Update("delete_at", value).Error; err != nil {
			t.Fatal(err)
		}
		var stored modelv1.StorageProbe
		if err := db.Model(&modelv1.StorageProbe{}).First(&stored, record.Id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.DeleteAt == nil || *stored.DeleteAt != value {
			t.Errorf("deletion time = %v, want present %d", stored.DeleteAt, value)
		}
	}
	if err := db.Model(&modelv1.StorageProbe{}).Where("id = ?", record.Id).
		Update("delete_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	var cleared modelv1.StorageProbe
	if err := db.Model(&modelv1.StorageProbe{}).First(&cleared, record.Id).Error; err != nil {
		t.Fatal(err)
	}
	if cleared.DeleteAt != nil {
		t.Errorf("cleared deletion time = %v, want SQL NULL", cleared.DeleteAt)
	}
}

func testTransactionRollback(t *testing.T, db *gorm.DB) {
	t.Helper()
	rollback := errors.New("probe rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&modelv1.StorageProbe{}).Create(newProbe("rollback")).Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Transaction(rollback) = %v, want original error", err)
	}
	var count int64
	if err := db.Model(&modelv1.StorageProbe{}).Where("instrument = ?", "rollback").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("rolled back records = %d, want 0", count)
	}
}

func newProbe(instrument string) *modelv1.StorageProbe {
	return &modelv1.StorageProbe{
		Venue:      probeVenue,
		Instrument: instrument,
		Price:      precisePrice,
		Enabled:    true,
		Revision:   7,
	}
}

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != code {
		t.Errorf("PostgreSQL error = %v, want SQLSTATE %s", err, code)
	}
}
