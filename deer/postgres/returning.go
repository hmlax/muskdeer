package postgres

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Every persistence model has one generated id. Application fields are supplied
// explicitly; database-assigned defaults are not an alternate parameter source.
// Restrict implicit RETURNING to identity, so reading historical schema versions
// does not ask PostgreSQL for newer default-only columns that were not inserted.
func identityReturning(db *gorm.DB) error {
	return db.Callback().Create().Before("gorm:create").Register("muskdeer:return_identity", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil {
			return
		}
		if _, explicit := tx.Statement.Clauses["RETURNING"]; explicit {
			return
		}
		id := tx.Statement.Schema.PrioritizedPrimaryField
		if id == nil || id.DBName != "id" {
			return
		}
		tx.Statement.AddClause(clause.Returning{Columns: []clause.Column{{Name: id.DBName}}})
	})
}
