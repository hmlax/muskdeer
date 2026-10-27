package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestInvalidMarketPublisherCannotReachDatabase(t *testing.T) {
	store, err := NewMarketSnapshots(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := store.Publish(ctx, domain.LeaseToken{}, 0, domain.MarketReading{}); !errors.Is(err, domain.ErrInvalidLease) {
		t.Fatal("missing authority reached database")
	}
	if _, err := store.Get(ctx, 0); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("missing feed reached database")
	}
	if _, err := store.Get(context.WithoutCancel(ctx), 1); !errors.Is(err, domain.ErrMarketIdentity) {
		t.Fatal("unbounded market query reached database")
	}
	cancel()
	if _, err := store.Get(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("market query cancellation was lost")
	}
}
