//go:build integration

package migrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

func TestStartupSchemaCheckIsReadOnlyAndRequiresExactHistory(t *testing.T) {
	db, _ := migrationDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := CheckCurrent(ctx, db); err == nil {
		t.Fatal("empty schema was accepted")
	}
	if db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Migrator().HasTable(&modelv1.SchemaMigration{}) {
		t.Fatal("startup schema validation created its ledger")
	}
	if _, err := Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := CheckCurrent(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Where("version = ?", 1).UpdateColumn("checksum", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if err := CheckCurrent(ctx, db); !errors.Is(err, ErrHistoryMismatch) {
		t.Fatalf("startup accepted changed migration history: %v", err)
	}
}
