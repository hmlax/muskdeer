package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/ownerid"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestInvalidCoreBoundaryCannotReachDatabase(t *testing.T) {
	core, err := NewSimulator(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	owner, err := ownerid.New(ctx, "boundary-test")
	if err != nil {
		t.Fatal(err)
	}
	valid := domain.GatewaySubmit{
		Input: domain.OrderInput{
			IdempotencyKey: "intent-1",
			Authority: domain.StrategyAuthority{
				StrategyID:    1,
				ConfigVersion: 1,
				OwnerID:       owner,
				OwnerEpoch:    1,
			},
			AccountID:    "account-1",
			InstrumentID: 1,
			Price:        decimal.NewFromInt(1),
			Quantity:     decimal.NewFromInt(1),
			Side:         domain.SideBuy,
			TimeInForce:  domain.TimeInForceGTC,
			Deadline:     time.Now().Add(500 * time.Millisecond),
		},
		ClientOrderID: "client-1",
		ExecutionAuthority: domain.LeaseToken{
			Key: domain.LeaseKey{
				Kind:      domain.LeaseKindExecution,
				AccountID: "account-1",
			},
			OwnerID: owner,
			Epoch:   1,
		},
	}
	for _, mutate := range []func(*domain.GatewaySubmit){
		func(c *domain.GatewaySubmit) { c.Input.Price = decimal.New(1, 1_000_000_000) },
		func(c *domain.GatewaySubmit) { c.Input.Quantity = decimal.Zero },
		func(c *domain.GatewaySubmit) { c.ExecutionAuthority.Key.AccountID = "another-account" },
		func(c *domain.GatewaySubmit) { c.Input.Authority.OwnerID = "reused-process-name" },
		func(c *domain.GatewaySubmit) { c.Input.Deadline = time.Now().Add(time.Minute) },
	} {
		candidate := valid
		mutate(&candidate)
		if _, err := core.Submit(ctx, candidate); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatalf("unsafe core input reached database: %v", err)
		}
	}
	if _, err := core.Submit(context.WithoutCancel(ctx), valid); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatalf("unbounded core work reached database: %v", err)
	}
	cancel()
	if _, err := core.Submit(ctx, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled core call lost its cause: %v", err)
	}
}

func TestCoreRejectionsKeepDatabaseCauseWithoutPublicSQL(t *testing.T) {
	cause := &pgconn.PgError{
		Code:    "23505",
		Message: "private SQL and credential text",
	}
	err := coreFailure(t.Context(), cause)
	var databaseError *pgconn.PgError
	if !errors.Is(err, domain.ErrIdempotencyConflict) || !errors.As(err, &databaseError) || err.Error() != domain.MessageIdempotencyConflict {
		t.Fatalf("core rejection lost its safe category/cause: %v", err)
	}
}
