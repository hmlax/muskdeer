package outbox

import (
	"context"
	"errors"

	"github.com/hmlax/muskdeer/deer/jetstream"
	commonv1 "github.com/hmlax/muskdeer/deer/proto/common/v1"
	"github.com/hmlax/muskdeer/internal/eventwire"
)

type publisher struct{ client *jetstream.Client }

// NewPublisher borrows the Deer-owned synchronous client and its stream scope.
func NewPublisher(client *jetstream.Client) (Publisher, error) {
	if client == nil {
		return nil, ErrInvalidRecord
	}
	return &publisher{client: client}, nil
}

func (p *publisher) Publish(ctx context.Context, event *commonv1.ExecutionEvent) error {
	if err := eventwire.Validate(ctx, event); err != nil {
		if errors.Is(err, eventwire.ErrInvalidEvent) {
			return &invalidEventError{cause: err}
		}
		return err
	}
	_, err := p.client.Publish(ctx, ExecutionSubject, event.EventId, event)
	return err
}

type invalidEventError struct{ cause error }

func (*invalidEventError) Error() string { return CodeInvalidEvent }
func (e *invalidEventError) Unwrap() error {
	return errors.Join(ErrInvalidEvent, e.cause)
}
