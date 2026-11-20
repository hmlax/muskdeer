// Package adminschema contains explicit administration schema maintenance.
package adminschema

import (
	"context"

	"gorm.io/gorm"
)

// ResetSequence advances an isolated schema's sequence after explicit ID seeding.
// Both identifiers come from the generated model, never from request input.
func ResetSequence(ctx context.Context, db *gorm.DB, model any, value int) error {
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	return db.WithContext(ctx).Model(model).Exec(
		"SELECT setval(pg_get_serial_sequence(?, ?), ?, true)",
		statement.Schema.Table, statement.Schema.PrioritizedPrimaryField.DBName, value,
	).Error
}
