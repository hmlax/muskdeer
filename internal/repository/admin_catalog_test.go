package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminCatalogRejectsUnboundedQueriesBeforeStorage(t *testing.T) {
	if _, err := NewAdminCatalog(nil); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("nil catalog storage accepted", err)
	}
	catalog, err := NewAdminCatalog(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for _, page := range []domain.AdminPage{
		{},
		{Limit: domain.MaxAdminPageSize + 1},
		{
			AfterID: -1,
			Limit:   1,
		},
	} {
		if _, err := catalog.Accounts(ctx, "", page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
			t.Fatal("invalid account page reached storage", err)
		}
		if _, err := catalog.Instruments(ctx, "", page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
			t.Fatal("invalid instrument page reached storage", err)
		}
		if _, err := catalog.Feeds(ctx, "", 1, domain.MarketModeReplay, page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
			t.Fatal("invalid feed page reached storage", err)
		}
	}
	page := domain.AdminPage{Limit: 1}
	if _, err := catalog.Feeds(ctx, "", 0, domain.MarketModeReplay, page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("invalid instrument reached storage", err)
	}
	if _, err := catalog.Feeds(ctx, "", 1, "", page); !errors.Is(err, domain.ErrInvalidAdminQuery) {
		t.Fatal("unspecified feed mode reached storage", err)
	}
	if _, err := catalog.Accounts(context.WithoutCancel(ctx), "", page); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("unbounded account query reached storage", err)
	}
	cancel()
	if _, err := catalog.Instruments(ctx, "", page); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled catalog query lost cause", err)
	}
}
