package deer

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
)

func TestRegistrationCanRequireConfiguredNacos(t *testing.T) {
	t.Parallel()
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("missing-required-nacos"), ServiceSpec{
		RegisterGRPC: func(_ context.Context, owner *Deer, _ grpc.ServiceRegistrar) error {
			_, err := owner.Discovery()
			return err
		},
	})
	if service != nil || cleanup != nil || !errors.Is(err, ErrNacosNotConfigured) {
		t.Fatalf("required nacos = %v, cleanup nil %v, %v", service, cleanup == nil, err)
	}
}
