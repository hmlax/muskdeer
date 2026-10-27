package rpcclient

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRemoteContextStatusBeforeLocalTimerKeepsCause(t *testing.T) {
	for _, test := range []struct {
		name  string
		code  codes.Code
		cause error
	}{
		{
			name:  "deadline",
			code:  codes.DeadlineExceeded,
			cause: context.DeadlineExceeded,
		},
		{
			name:  "cancellation",
			code:  codes.Canceled,
			cause: context.Canceled,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			remote := status.Error(test.code, "remote stream terminated")
			result := contextualError(t.Context(), remote)
			if !errors.Is(result, test.cause) || !errors.Is(result, remote) || status.Code(result) != test.code {
				t.Fatalf("context status lost its canonical cause: %v", result)
			}
			if t.Context().Err() != nil {
				t.Fatal("test requires a still-active caller context")
			}
		})
	}
	denied := status.Error(codes.PermissionDenied, "remote rejected identity")
	if contextualError(t.Context(), denied) != denied || contextualError(t.Context(), nil) != nil {
		t.Fatal("normalization changed an unrelated failure or successful call")
	}
}
