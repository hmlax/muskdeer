package migrate

import (
	"context"
	"errors"

	"gorm.io/gorm"

	modelv1 "github.com/hmlax/muskdeer/deer/proto/model/v1"
)

type checkError struct{ cause error }

func (*checkError) Error() string   { return "schema validation failed" }
func (e *checkError) Unwrap() error { return e.cause }

// CheckCurrent only reads the ledger and requires this binary's exact complete
// history. It never bootstraps the ledger, migrates DDL or repairs checksums.
func CheckCurrent(ctx context.Context, db *gorm.DB) error {
	if ctx == nil || db == nil {
		return errors.New("schema validation context and database are required")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return errors.New("schema validation requires a bounded caller context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	definitions, err := loadDefinitions()
	if err != nil {
		return err
	}
	var history []*modelv1.SchemaMigration
	err = db.WithContext(ctx).Model(&modelv1.SchemaMigration{}).Order("version ASC").Limit(len(definitions) + 1).Find(&history).Error
	if err != nil {
		return &checkError{cause: errors.Join(err, ctx.Err())}
	}
	if len(history) != len(definitions) {
		return ErrHistoryMismatch
	}
	return validateHistory(history, definitions)
}
