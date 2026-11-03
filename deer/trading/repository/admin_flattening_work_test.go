package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/trading/domain"
)

func TestAdminFlattenWorkBorrowsStorageAndRejectsCanceledClaimBeforeIO(t *testing.T) {
	t.Parallel()
	if _, err := NewAdminFlattenWork(nil); !errors.Is(err, domain.ErrInvalidAdminFlatten) {
		t.Fatal("nil flatten work database was accepted", err)
	}
	store, err := NewAdminFlattenWork(&gorm.DB{})
	if err != nil {
		t.Fatal("flatten work constructor performed IO", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	cancel()
	if got, err := store.Claim(ctx, domain.LeaseToken{}, 0); !errors.Is(err, context.Canceled) || got.ID != 0 {
		t.Fatal("canceled claim reached storage or returned partial work", err)
	}
}
