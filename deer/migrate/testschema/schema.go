//go:build integration

// Package testschema owns isolated SQL namespaces for integration fixtures.
package testschema

import (
	"context"
	"errors"
	"regexp"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"

	model "github.com/hmlax/muskdeer/deer/pb/common/model/v1"
)

var testName = regexp.MustCompile("^muskdeer_test_[a-z0-9_]+$")

// Create installs only an explicitly named test namespace.
func Create(ctx context.Context, db *gorm.DB, name string) error {
	if !testName.MatchString(name) {
		return errors.New("isolated test schema required")
	}
	return db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec("CREATE SCHEMA " + pgx.Identifier{name}.Sanitize()).Error
}

// Drop removes only that test namespace after all fixture work has joined.
func Drop(ctx context.Context, db *gorm.DB, name string) error {
	if !testName.MatchString(name) {
		return errors.New("isolated test schema required")
	}
	return db.WithContext(ctx).Model(&model.SchemaMigration{}).Exec("DROP SCHEMA " + pgx.Identifier{name}.Sanitize() + " CASCADE").Error
}
