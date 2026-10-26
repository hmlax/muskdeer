package deer

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
)

func TestRegistrationCanRequireConfiguredPostgres(t *testing.T) {
	t.Parallel()
	service, cleanup, err := NewDeer(t.Context(), serviceConfig("missing-required-postgres"), ServiceSpec{
		RegisterGRPC: func(ctx context.Context, owner *Deer, _ grpc.ServiceRegistrar) error {
			if _, exists := ctx.Deadline(); !exists || owner.Logger() == nil {
				return errors.New("registration did not receive initialized service dependencies")
			}
			_, err := owner.DB()
			return err
		},
	})
	if service != nil || cleanup != nil || !errors.Is(err, ErrPostgresNotConfigured) {
		t.Fatalf("required postgres = %v, cleanup nil %v, %v", service, cleanup == nil, err)
	}
}
