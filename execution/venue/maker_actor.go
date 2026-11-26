package venue

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	common "github.com/hmlax/muskdeer/deer/pb/common/v1"
	pb "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	"github.com/hmlax/muskdeer/deer/tracing"
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
	hadTarget := false
	return r.mailbox.run(ctx, r.cfg.OperationTimeout, func(work context.Context, plan *pb.QuotePlan) error {
		work, span := r.factory.StartOperation(work, "execution.quote")
		defer span.End()
		span.SetAttributes(attribute.String("market.symbol", profile.Symbol))
		if plan == nil && hadTarget {
			if err := r.logger.LogAttrs(work, slog.LevelWarn, native.StateRecovering, slog.String("symbol", profile.Symbol), slog.String("reason", reasonQuoteTargetExpired)); err != nil {
				return err
			}
		}
		hadTarget = plan != nil
		err := r.applyMakerIntent(work, profile, plan)
		span.SetAttributes(attribute.String("quote.step", quoteFailureStep(err)), attribute.String("quote.reason", safeReason(err)))
		tracing.RecordError(work, err)
		if err != nil && !failed && ctx.Err() == nil {
			if logErr := r.logger.LogAttrs(context.WithoutCancel(work), slog.LevelWarn, native.StateRecovering, slog.String("symbol", profile.Symbol), slog.String("reason", safeReason(err)), slog.String("step", quoteFailureStep(err))); logErr != nil {
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
		return quoteFailed(quoteStepProtection, r.protectMakerIntent(ctx, profile))
	}
	work, span := tracing.StartChild(ctx, "execution.quote.reconcile")
	_, reconcileErr := r.reconcile(work, profile, plan)
	span.SetAttributes(attribute.String("quote.step", quoteFailureStep(reconcileErr)), attribute.String("quote.reason", safeReason(reconcileErr)))
	tracing.RecordError(work, reconcileErr)
	span.End()
	if reconcileErr == nil || ctx.Err() != nil {
		return reconcileErr
	}
	// Failed replacement must still run risk reduction under the same writer
	// and deadline. No source, account or configuration guard is bypassed.
	return errors.Join(reconcileErr, quoteFailed(quoteStepProtection, r.protectMakerIntent(ctx, profile)))
}

func (r *Runtime) protectMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy) (err error) {
	work, span := tracing.StartChild(ctx, "execution.quote.protection")
	defer func() {
		span.SetAttributes(attribute.String("quote.reason", safeReason(err)))
		tracing.RecordError(work, err)
		span.End()
	}()
	return r.protect(work, profile)
}

func (r *Runtime) newerMakerIntent(previous *pb.QuotePlan) *pb.QuotePlan {
	if r.mailbox == nil {
		return nil
	}
	next := r.mailbox.pending.Load()
	if next == nil || next == previous || next.GetBook().GetValidUntilMs() <= time.Now().UnixMilli() {
		return nil
	}
	if next.Book.Sequence > previous.GetBook().GetSequence() || next.Book.Sequence == previous.GetBook().GetSequence() && quoteObservedAt(next) > quoteObservedAt(previous) {
		return next
	}
	return nil
}

// Account observation may have waited on settlement. Select the newest accepted
// target before computing any sends/retirements; the account timestamps stay as
// returned by that observation and still bound every SDK send.
func (r *Runtime) latestMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy, previous *pb.QuotePlan, book *common.VenueBook) (*pb.QuotePlan, *common.VenueBook, error) {
	if !r.directMaker() {
		return previous, book, nil
	}
	if r.newerMakerIntent(previous) == nil {
		if min(book.GetValidUntilMs(), previous.GetBook().GetValidUntilMs()) <= time.Now().UnixMilli() {
			return nil, nil, native.ErrInvalid
		}
		return previous, book, nil
	}
	fresh, err := r.book(ctx, profile.Symbol)
	if err != nil {
		return nil, nil, err
	}
	return r.adoptMakerIntent(ctx, profile, previous, fresh)
}

func (r *Runtime) adoptMakerIntent(ctx context.Context, profile *common.VenueProfilePolicy, previous *pb.QuotePlan, fresh *common.VenueBook) (*pb.QuotePlan, *common.VenueBook, error) {
	next := r.newerMakerIntent(previous)
	if next == nil {
		next = previous
	}
	if fresh.GetValidUntilMs() <= time.Now().UnixMilli() {
		return nil, nil, native.ErrInvalid
	}
	if err := r.validatePlanAuthority(ctx, profile, next, fresh); err != nil {
		return nil, nil, err
	}
	fresh.ValidUntilMs = min(fresh.ValidUntilMs, next.Book.ValidUntilMs)
	return next, fresh, nil
}

func (r *Runtime) quoteBook(ctx context.Context, profile *common.VenueProfilePolicy, plan *pb.QuotePlan) (*common.VenueBook, error) {
	book, err := r.book(ctx, profile.Symbol)
	if err != nil {
		return nil, err
	}
	if err := r.validatePlanAuthority(ctx, profile, plan, book); err != nil {
		return nil, err
	}
	book.ValidUntilMs = min(book.ValidUntilMs, plan.Book.ValidUntilMs)
	return book, nil
}
