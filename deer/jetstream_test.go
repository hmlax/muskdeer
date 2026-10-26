package deer

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
)

func TestRegistrationCanRequireConfiguredJetStream(t *testing.T) {
	t.Parallel()
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("missing-required-jetstream"), ServiceSpec{
		RegisterGRPC: func(_ context.Context, owner *Deer, _ grpc.ServiceRegistrar) error {
			_, err := owner.JetStream()
			return err
		},
	})
	if service != nil || cleanup != nil || !errors.Is(err, ErrJetStreamNotConfigured) {
		t.Fatalf("required jetstream = %v, cleanup nil %v, %v", service, cleanup == nil, err)
	}
}
