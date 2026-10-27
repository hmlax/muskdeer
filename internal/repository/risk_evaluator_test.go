package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/internal/domain"
)

func TestRiskEvaluatorRejectsUnboundedCanceledOrInvalidWorkBeforeDatabaseIO(t *testing.T) {
	if _, err := NewRiskEvaluator(nil); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("nil database accepted")
	}
	evaluator, err := NewRiskEvaluator(&gorm.DB{})
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range []context.Context{nil, context.Background()} {
		if _, err := evaluator.Evaluate(ctx, 1); !errors.Is(err, domain.ErrInvalidOrder) {
			t.Fatal("unbounded evaluation reached database")
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	if _, err := evaluator.Evaluate(ctx, 0); !errors.Is(err, domain.ErrInvalidOrder) {
		t.Fatal("invalid identity reached database")
	}
	cancel()
	if _, err := evaluator.Evaluate(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled evaluation reached database")
	}
}

func TestRiskGrantExpiryUsesEarliestAuthorityDataOrInstanceLimit(t *testing.T) {
	now := time.UnixMilli(10000).UTC()
	reference := domain.MarketReference{
		AuthorityUntil: now.Add(4 * time.Second),
		Reading: domain.MarketReading{
			EventTime:  now.Add(-400 * time.Millisecond),
			ReceivedAt: now,
			ValidUntil: now.Add(3 * time.Second),
		},
	}
	if got := riskGrantExpiry(now, reference, 500*time.Millisecond); !got.Equal(now.Add(100 * time.Millisecond)) {
		t.Fatalf("old reference extended by grant refresh: %v", got)
	}
	reference.AuthorityUntil = now.Add(20 * time.Millisecond)
	if got := riskGrantExpiry(now, reference, 500*time.Millisecond); !got.Equal(reference.AuthorityUntil) {
		t.Fatal("grant exceeded publisher authority")
	}
	reference.AuthorityUntil = now.Add(4 * time.Second)
	reference.Reading.ValidUntil = now.Add(10 * time.Millisecond)
	if got := riskGrantExpiry(now, reference, 500*time.Millisecond); !got.Equal(reference.Reading.ValidUntil) {
		t.Fatal("grant exceeded stored validity")
	}
	reference.AuthorityUntil = now.Add(time.Minute)
	reference.Reading.ValidUntil = now.Add(time.Minute)
	reference.Reading.EventTime, reference.Reading.ReceivedAt = now, now
	if got := riskGrantExpiry(now, reference, time.Minute); !got.Equal(now.Add(domain.MaxRiskSnapshotAge)) {
		t.Fatal("grant exceeded hard maximum risk age")
	}
}
