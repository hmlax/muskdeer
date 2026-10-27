// Package migrate applies embedded, checksummed PostgreSQL schema upgrades.
// Services never invoke it automatically; the operator command owns execution.
package migrate

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

// ErrHistoryMismatch requires operator review; applied history is never repaired.
var ErrHistoryMismatch = errors.New("schema migration history differs from this binary")

//go:embed sql/*.sql
var definitionsFS embed.FS

const (
	migrationLockNamespace int32 = 0x4d55534b
	migrationLockKey       int32 = 0x44454552
)

type definition struct {
	version  int64
	name     string
	sql      string
	checksum string
}

type applyError struct{ cause error }

func (*applyError) Error() string   { return "schema migration failed" }
func (e *applyError) Unwrap() error { return e.cause }

// Apply serializes migration writers and atomically applies all pending schema
// versions plus their generated ledger records. A failed/ambiguous commit can
// be retried by this same binary: persisted checksums determine what remains.
// db is borrowed; callers must provide a bounded context and own pool cleanup.
func Apply(ctx context.Context, db *gorm.DB) (int64, error) {
	if ctx == nil || db == nil {
		return 0, errors.New("migration context and database are required")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return 0, errors.New("migration requires a bounded caller context")
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	definitions, err := loadDefinitions()
	if err != nil {
		return 0, err
	}
	if err := applyDefinitions(ctx, db, definitions); err != nil {
		return 0, &applyError{cause: errors.Join(err, ctx.Err())}
	}
	return definitions[len(definitions)-1].version, nil
}

func applyDefinitions(ctx context.Context, db *gorm.DB, definitions []definition) error {
	return db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Transaction(func(tx *gorm.DB) error {
		// Fixed advisory-lock SQL has no table selector or business DML. The
		// lock is released by transaction end, including cancellation/rollback.
		if err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).
			Exec("SELECT pg_advisory_xact_lock(?, ?)", migrationLockNamespace, migrationLockKey).Error; err != nil {
			return err
		}
		hadLedger := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Migrator().HasTable(&modelv1.SchemaMigration{})
		// The first reviewed DDL bootstraps only the ledger, before any read.
		// IF NOT EXISTS permits subsequent validation without altering history.
		if err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Exec(definitions[0].sql).Error; err != nil {
			return err
		}
		var applied []*modelv1.SchemaMigration
		if err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Order("version ASC").Find(&applied).Error; err != nil {
			return err
		}
		if hadLedger && len(applied) == 0 {
			return ErrHistoryMismatch
		}
		if err := validateHistory(applied, definitions); err != nil {
			return err
		}
		for index := len(applied); index < len(definitions); index++ {
			current := definitions[index]
			if index != 0 {
				if err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Exec(current.sql).Error; err != nil {
					return err
				}
			}
			row := &modelv1.SchemaMigration{
				Version:  current.version,
				Name:     current.name,
				Checksum: current.checksum,
			}
			if err := tx.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Create(row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func loadDefinitions() ([]definition, error) {
	names, err := fs.Glob(definitionsFS, "sql/*.sql")
	if err != nil || len(names) == 0 {
		return nil, errors.New("embedded schema migrations are missing")
	}
	definitions := make([]definition, 0, len(names))
	for index, name := range names {
		base := path.Base(name)
		prefix, _, found := strings.Cut(base, "_")
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || !found || version != int64(index+1) || len(base) > 128 {
			return nil, errors.New("embedded schema migration sequence is invalid")
		}
		data, err := definitionsFS.ReadFile(name)
		if err != nil || len(data) == 0 {
			return nil, errors.New("embedded schema migration cannot be read")
		}
		definitions = append(definitions, definition{
			version:  version,
			name:     base,
			sql:      string(data),
			checksum: fmt.Sprintf("%x", sha256.Sum256(data)),
		})
	}
	return definitions, nil
}

func validateHistory(applied []*modelv1.SchemaMigration, definitions []definition) error {
	if len(applied) > len(definitions) {
		return ErrHistoryMismatch
	}
	for index, row := range applied {
		expected := definitions[index]
		if row == nil || row.DeleteAt != nil || row.Version != expected.version || row.Name != expected.name || row.Checksum != expected.checksum {
			return ErrHistoryMismatch
		}
	}
	return nil
}
