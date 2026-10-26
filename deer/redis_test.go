package deer

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
)

func TestRegistrationCanRequireConfiguredRedis(t *testing.T) {
	t.Parallel()
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("missing-required-redis"), ServiceSpec{
		RegisterGRPC: func(_ context.Context, owner *Deer, _ grpc.ServiceRegistrar) error {
			_, err := owner.Redis()
			return err
		},
	})
	if service != nil || cleanup != nil || !errors.Is(err, ErrRedisNotConfigured) {
		t.Fatalf("required redis = %v, cleanup nil %v, %v", service, cleanup == nil, err)
	}
}
