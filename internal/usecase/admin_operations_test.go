package usecase

import (
	"context"
	"errors"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

type operationObserver struct {
	rows     []int64
	observed []int64
	delay    time.Duration
	stop     context.CancelFunc
}

func (o *operationObserver) Pending(ctx context.Context, after int64, limit int) ([]int64, error) {
	if err := o.wait(ctx); err != nil {
		return nil, err
	}
	result := make([]int64, 0, limit)
	for _, id := range o.rows {
		if id > after && len(result) < limit {
			result = append(result, id)
		}
	}
	return result, nil
}

func (o *operationObserver) Observe(ctx context.Context, id int64) (bool, error) {
	o.observed = append(o.observed, id)
	if o.stop != nil {
		o.stop()
		return false, nil
	}
	return id == 17, o.wait(ctx)
}

func (o *operationObserver) wait(ctx context.Context) error {
	select {
	case <-time.After(o.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestOperationProjectorDoesNotStarveBehindUnreadyFirstPage(t *testing.T) {
	observer := &operationObserver{}
	for id := int64(1); id <= 17; id++ {
		observer.rows = append(observer.rows, id)
	}
	projector, err := NewAdminOperationProjector(observer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for index, expectedWork := range []bool{false, true, false, false} {
		if worked, err := projector.Tick(t.Context()); err != nil || worked != expectedWork {
			t.Fatalf("cursor stopped at an unready receipt: tick=%d worked=%t err=%v", index, worked, err)
		}
	}
	want := append(slices.Clone(observer.rows), observer.rows[:16]...)
	if !slices.Equal(observer.observed, want) {
		t.Fatal("projector skipped later receipts or failed to revisit pending ones")
	}
}

func TestOperationProjectorUsesFreshNativeBudgetsAndStopsOnParentCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		observer := &operationObserver{
			rows:  []int64{1, 2},
			delay: 600 * time.Millisecond,
		}
		projector, err := NewAdminOperationProjector(observer, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := projector.Tick(t.Context()); err != nil || len(observer.observed) != 2 {
			t.Fatal("native budgets were shared across unrelated receipts", err)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observer := &operationObserver{
		rows: []int64{1, 2},
		stop: cancel,
	}
	projector, err := NewAdminOperationProjector(observer, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := projector.Tick(ctx); !errors.Is(err, context.Canceled) || !slices.Equal(observer.observed, []int64{1}) {
		t.Fatal("canceled lifecycle continued to another operation", err)
	}
}

var _ domain.AdminOperationObserver = (*operationObserver)(nil)
