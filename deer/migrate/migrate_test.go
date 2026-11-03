package migrate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

func TestApplyRejectsUnboundedAndCanceledWorkBeforeDatabaseIO(t *testing.T) {
	db := &gorm.DB{}
	if _, err := Apply(t.Context(), db); err == nil {
		t.Fatal("migration accepted an unbounded caller context")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	cancel()
	if _, err := Apply(ctx, db); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled migration started I/O or lost its cause: %v", err)
	}
}

func TestMigrationHistoryCannotSkipRewriteOrHideAppliedVersions(t *testing.T) {
	definitions, err := loadDefinitions()
	if err != nil || len(definitions) == 0 {
		t.Fatalf("embedded definitions: %v", err)
	}
	first := definitions[0]
	valid := &modelv1.SchemaMigration{
		Version:  first.version,
		Name:     first.name,
		Checksum: fmt.Sprintf("%x", sha256.Sum256([]byte(first.sql))),
	}
	if err := validateHistory([]*modelv1.SchemaMigration{valid}, definitions); err != nil {
		t.Fatal(err)
	}
	for _, row := range []*modelv1.SchemaMigration{
		{
			Version:  first.version,
			Name:     first.name,
			Checksum: "changed",
		},
		{
			Version:  first.version + 1,
			Name:     first.name,
			Checksum: valid.Checksum,
		},
		{
			Version:  first.version,
			Name:     "renamed.sql",
			Checksum: valid.Checksum,
		},
	} {
		if err := validateHistory([]*modelv1.SchemaMigration{row}, definitions); !errors.Is(err, ErrHistoryMismatch) {
			t.Fatalf("changed history was accepted: %v", err)
		}
	}
}
