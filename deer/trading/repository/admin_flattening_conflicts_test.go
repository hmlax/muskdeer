package repository

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestPendingFlatteningIsAnExplicitCreationAndPublicationConflict(t *testing.T) {
	t.Parallel()
	for _, classify := range []func(error) error{adminCreationFailure, adminPublicationFailure} {
		failure := &pgconn.PgError{
			Code:           "23514",
			ConstraintName: "admin_flatten_pending",
			Message:        "private storage detail",
		}
		if result := classify(failure); !errors.Is(result, domain.ErrAdminConflict) || result.Error() != domain.ErrAdminConflict.Error() {
			t.Fatal("pending flattening did not become an explicit safe conflict", result)
		}
		failure.ConstraintName = "unrelated_database_constraint"
		if errors.Is(classify(failure), domain.ErrAdminConflict) {
			t.Fatal("unrelated storage failure was hidden as a retryable conflict")
		}
	}
}
