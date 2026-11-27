package deer

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	execution "github.com/hmlax/muskdeer/deer/pb/execution/venue/v1"
	market "github.com/hmlax/muskdeer/deer/pb/market/venue/v1"
	"github.com/hmlax/muskdeer/deer/rpcsecurity"
)

const streamDeadlineRequired = "stream requires a client deadline within the configured request timeout"

const maximumQuoteDiagnosticStream = 15 * time.Minute

func streamDeadlineInterceptor(maximum time.Duration) grpc.StreamServerInterceptor {
	return func(service any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Authenticated market reads last for the caller's subscription scope.
		// Unary operations and all other streams keep their existing deadlines.
		if info != nil && (info.FullMethod == market.Market_SubscribeBooks_FullMethodName || info.FullMethod == market.Market_SubscribeTrades_FullMethodName) && (rpcsecurity.HasService(stream.Context(), rpcsecurity.ServiceStrategy) || rpcsecurity.HasService(stream.Context(), rpcsecurity.ServiceExecution)) {
			return handler(service, stream)
		}
		limit := maximum
		if info != nil && info.IsServerStream && info.FullMethod == execution.Execution_SubscribeQuoteFlows_FullMethodName && rpcsecurity.HasService(stream.Context(), rpcsecurity.ServiceStrategy) {
			// A diagnostic has a native client deadline too: blocked SendMsg
			// must terminate without changing any financial RPC's timeout.
			limit = maximumQuoteDiagnosticStream
		}
		deadline, exists := stream.Context().Deadline()
		if !exists || time.Until(deadline) > limit {
			return status.Error(codes.InvalidArgument, streamDeadlineRequired)
		}
		// A native client deadline also interrupts blocked RecvMsg/SendMsg.
		// Merely replacing ServerStream.Context would not interrupt that I/O.
		return handler(service, stream)
	}
}
