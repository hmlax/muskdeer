package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestAdminPublicationRejectsInvalidOrUnboundedInputBeforeStorage(t *testing.T) {
	if _, err := NewAdminStrategyPublisher(nil); !errors.Is(err, domain.ErrInvalidAdminPublication) {
		t.Fatal("nil publication storage accepted", err)
	}
	publisher, err := NewAdminStrategyPublisher(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := publisher.Publish(ctx, "", "", domain.AdminPublishStrategy{}); !errors.Is(err, domain.ErrInvalidAdminPublication) {
		t.Fatal("invalid publication reached storage", err)
	}
	if _, err := publisher.Publish(context.WithoutCancel(ctx), "", "", domain.AdminPublishStrategy{}); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("unbounded publication reached storage", err)
	}
	cancel()
	if _, err := publisher.Publish(ctx, "", "", domain.AdminPublishStrategy{}); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled publication lost native cancellation", err)
	}
}
