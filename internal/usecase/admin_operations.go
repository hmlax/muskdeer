package usecase

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/internal/domain"
)

const adminObservationPageSize = 16

// AdminOperationProjector belongs to one serial worker. Its cursor only
// schedules observations; every effect/result remains authoritative in storage.
type AdminOperationProjector struct {
	observer domain.AdminOperationObserver
	timeout  time.Duration
	after    int64
}

// NewAdminOperationProjector starts no work and owns no connection or goroutine.
func NewAdminOperationProjector(observer domain.AdminOperationObserver, timeout time.Duration) (*AdminOperationProjector, error) {
	if observer == nil || timeout < 100*time.Millisecond || timeout > 5*time.Second || timeout%time.Millisecond != 0 {
		return nil, domain.ErrInvalidAdminControl
	}
	return &AdminOperationProjector{
		observer: observer,
		timeout:  timeout,
	}, nil
}

// Tick advances past unready operations instead of starving later receipts.
// Each storage call has a fresh native budget, bounded by lifecycle cancellation.
func (p *AdminOperationProjector) Tick(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, domain.ErrInvalidAdminControl
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	query, cancel := context.WithTimeout(ctx, p.timeout)
	ids, err := p.observer.Pending(query, p.after, adminObservationPageSize)
	cancel()
	if err != nil {
		return false, err
	}
	if len(ids) > adminObservationPageSize {
		return false, domain.ErrInvalidAdminControl
	}
	if len(ids) == 0 {
		p.after = 0
		return false, nil
	}
	worked := false
	for _, id := range ids {
		if id <= p.after {
			return false, domain.ErrInvalidAdminControl
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		call, cancelCall := context.WithTimeout(ctx, p.timeout)
		completed, err := p.observer.Observe(call, id)
		cancelCall()
		if err != nil {
			return false, err
		}
		p.after = id
		worked = worked || completed
	}
	return worked, nil
}
