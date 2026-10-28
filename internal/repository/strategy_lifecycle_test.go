package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestLifecycleBoundaryRejectsInvalidAuthorityBeforeStorage(t *testing.T) {
	lifecycle, err := NewStrategyLifecycle(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	current := domain.StrategySpec{
		ID:       1,
		Revision: 1,
	}
	token := domain.LeaseToken{
		Key: domain.LeaseKey{
			Kind:       domain.LeaseKindStrategy,
			StrategyID: 1,
		},
		OwnerID: "strategy.01234567890123456789012345678901",
		Epoch:   1,
	}
	transition := domain.StrategyTransition{State: domain.StrategyStateRecovering}
	for _, mutate := range []func(*domain.LeaseToken){
		func(v *domain.LeaseToken) { v.OwnerID = "reused-name" },
		func(v *domain.LeaseToken) { v.Epoch = 0 },
		func(v *domain.LeaseToken) { v.Key.Kind = domain.LeaseKindExecution },
		func(v *domain.LeaseToken) { v.Key.StrategyID = 2 },
		func(v *domain.LeaseToken) { v.Key.AccountID = "another-scope" },
	} {
		candidate := token
		mutate(&candidate)
		if _, err := lifecycle.Transition(ctx, current, candidate, transition); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatal("invalid lifecycle authority reached storage")
		}
	}
	if _, err := lifecycle.Transition(context.WithoutCancel(ctx), current, token, transition); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("unbounded lifecycle operation reached storage")
	}
}
