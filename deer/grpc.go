package deer

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const streamDeadlineRequired = "stream requires a client deadline within the configured request timeout"

func streamDeadlineInterceptor(maximum time.Duration) grpc.StreamServerInterceptor {
	return func(service any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		deadline, exists := stream.Context().Deadline()
		if !exists || time.Until(deadline) > maximum {
			return status.Error(codes.InvalidArgument, streamDeadlineRequired)
		}
		// A native client deadline also interrupts blocked RecvMsg/SendMsg.
		// Merely replacing ServerStream.Context would not interrupt that I/O.
		return handler(service, stream)
	}
}
