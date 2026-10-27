// Package simulator supplies explicit, deterministic simulator fault controls.
// It never retries or changes the stable identity of a trading operation.
package simulator

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/internal/domain"
)

// ResponseFault selects behavior only after a successful core commit.
type ResponseFault string

// Response modes are explicit; the empty value cannot configure a scenario.
const (
	ResponseNormal          ResponseFault = "normal"
	ResponseLost            ResponseFault = "lost"
	ResponseWait            ResponseFault = "wait"
	messageInvalidFaultPlan               = "invalid simulator response fault plan"
)

// FaultPlan borrows scenario channels; callers must keep them open until all
// calls join. Committed signals a faulted operation's successful core return;
// Gate releases a delayed reply. No timer, worker or retry is created here.
type FaultPlan struct {
	Submit    ResponseFault
	Cancel    ResponseFault
	Gate      <-chan struct{}
	Committed chan<- struct{}
}

type faultGateway struct {
	domain.TradingGateway
	plan FaultPlan
}

type outcomeError struct{ cause error }

func (*outcomeError) Error() string { return domain.MessageOutcomeUnknown }
func (e *outcomeError) Unwrap() error {
	return errors.Join(domain.ErrOutcomeUnknown, e.cause)
}

// WithFaults decorates an actual trading gateway. Query paths remain available;
// known core rejections pass through unchanged. Use only in simulator wiring.
func WithFaults(gateway domain.TradingGateway, plan FaultPlan) (domain.TradingGateway, error) {
	if gateway == nil || !validMode(plan.Submit) || !validMode(plan.Cancel) ||
		(plan.Submit == ResponseWait || plan.Cancel == ResponseWait) && plan.Gate == nil {
		return nil, errors.New(messageInvalidFaultPlan)
	}
	return &faultGateway{
		TradingGateway: gateway,
		plan:           plan,
	}, nil
}

func (g *faultGateway) Submit(ctx context.Context, command domain.GatewaySubmit) (domain.CoreOrder, error) {
	if err := validCall(ctx); err != nil {
		return domain.CoreOrder{}, err
	}
	order, err := g.TradingGateway.Submit(ctx, command)
	if err != nil {
		return domain.CoreOrder{}, err
	}
	if err := g.after(ctx, g.plan.Submit); err != nil {
		return domain.CoreOrder{}, err
	}
	return order, nil
}

func (g *faultGateway) Cancel(ctx context.Context, command domain.GatewayCancel) (domain.CoreOrder, error) {
	if err := validCall(ctx); err != nil {
		return domain.CoreOrder{}, err
	}
	order, err := g.TradingGateway.Cancel(ctx, command)
	if err != nil {
		return domain.CoreOrder{}, err
	}
	if err := g.after(ctx, g.plan.Cancel); err != nil {
		return domain.CoreOrder{}, err
	}
	return order, nil
}

func (g *faultGateway) after(ctx context.Context, mode ResponseFault) error {
	if mode == ResponseNormal {
		return nil
	}
	if g.plan.Committed != nil {
		select {
		case g.plan.Committed <- struct{}{}:
		case <-ctx.Done():
			return &outcomeError{cause: errors.Join(ctx.Err(), context.Cause(ctx))}
		}
	}
	if mode == ResponseLost {
		return &outcomeError{cause: context.DeadlineExceeded}
	}
	select {
	case <-g.plan.Gate:
		if err := ctx.Err(); err != nil {
			return &outcomeError{cause: errors.Join(err, context.Cause(ctx))}
		}
		return nil
	case <-ctx.Done():
		return &outcomeError{cause: errors.Join(ctx.Err(), context.Cause(ctx))}
	}
}

func validMode(mode ResponseFault) bool {
	return mode == ResponseNormal || mode == ResponseLost || mode == ResponseWait
}

func validCall(ctx context.Context) error {
	if ctx == nil {
		return domain.ErrInvalidOrder
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return domain.ErrInvalidOrder
	}
	return ctx.Err()
}
