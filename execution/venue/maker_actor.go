package venue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/trading/domain"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

func (r *Runtime) queueMakerQuote(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan) (*pb.QuoteResult, error) {
	if r.mailbox == nil || r.closing.Load() || r.client.Load() == nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	book, err := r.book(ctx, profile.Symbol)
	if err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	if err := r.admitMakerIntent(ctx, profile, plan, book); err != nil {
		return nil, status.Error(codes.Unavailable, native.MessageInvalid)
	}
	// Accepted targets are pending work, never a reported SDK success. Actual
	// financial results remain in matching observations and the writer's logs.
	return &pb.QuoteResult{State: native.StateRecovering}, nil
}

func (r *Runtime) admitMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan, book *common.VenueBook) error {
	if err := r.validateMakerIntent(ctx, profile, plan, book); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.mailbox == nil || r.closing.Load() || book.ValidUntilMs <= time.Now().UnixMilli() || plan.Book.ValidUntilMs <= time.Now().UnixMilli() {
		return native.ErrInvalid
	}
	if r.hasFreshObservation() {
		r.rememberQuotePlan(profile, plan, book)
	}
	r.mailbox.offer(plan)
	return nil
}

func (r *Runtime) maintainMaker(ctx context.Context) error {
	if r.mailbox == nil || len(r.cfg.Venue.Profiles) != 1 {
		return ErrInvalidConfig
	}
	profile := r.cfg.Venue.Profiles[0]
	failed := false
	return r.mailbox.run(ctx, r.cfg.OperationTimeout, func(work context.Context, plan *pb.QuotePlan) error {
		err := r.applyMakerIntent(work, profile, plan)
		if err != nil && !failed && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(ctx, slog.LevelWarn, native.StateRecovering, slog.String("symbol", profile.Symbol), slog.String("reason", safeReason(err))); logErr != nil {
				return logErr
			}
		}
		failed = err != nil
		return nil
	})
}

func (r *Runtime) applyMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan) error {
	leave, err := r.enter(native.Scope(profile))
	if errors.Is(err, domain.ErrLeaseHeld) {
		return nil
	}
	if err != nil {
		return err
	}
	defer leave()
	if plan == nil {
		return r.protect(ctx, profile)
	}
	_, reconcileErr := r.reconcile(ctx, profile, plan)
	if reconcileErr == nil || ctx.Err() != nil {
		return reconcileErr
	}
	// Failed replacement must still run risk reduction under the same writer
	// and deadline. No source, account or configuration guard is bypassed.
	return errors.Join(reconcileErr, r.protect(ctx, profile))
}
