package executiongrpc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/rpcsecurity"
	"github.com/hmlax/muskdeer/internal/domain"
)

func TestRPCFailureClassificationHidesDatabaseAndTransportText(t *testing.T) {
	sensitive := errors.New("password=do-not-log private query arguments")
	for _, test := range []struct {
		cause error
		code  codes.Code
	}{
		{
			cause: sensitive,
			code:  codes.Unavailable,
		},
		{
			cause: errors.Join(sensitive, domain.ErrRiskDenied),
			code:  codes.FailedPrecondition,
		},
		{
			cause: errors.Join(sensitive, context.DeadlineExceeded),
			code:  codes.DeadlineExceeded,
		},
	} {
		err := rpcError(test.cause)
		if status.Code(err) != test.code || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "do-not-log") {
			t.Fatalf("unsafe RPC error: %v", err)
		}
	}
}

func TestMetadataCannotGrantMethodPermission(t *testing.T) {
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs("x-service-id", rpcsecurity.ServiceStrategy))
	if err := authorized(ctx, true); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("metadata granted permission: %v", err)
	}
}
