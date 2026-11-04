package himalaya

import (
	"context"
	"io"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type sdkDispatchContextKey struct{}

type sdkDispatchTrace struct {
	entered atomic.Bool
	body    atomic.Bool
}

const (
	sdkAuthorizationHeader = "authorization"
	sdkProtocolHeader      = "x-orderbook-protocol"
	messageMultipleReplies = "himalaya SDK unary call returned multiple replies"
)

// invokeSDKOnce uses the provided SDK's dial/interceptor extension point.
// Commit the stream before SendMsg, as in deer/rpcclient, to prevent transparent
// replay of a business request; WithDisableRetry alone does not prevent it.
func invokeSDKOnce(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, _ grpc.UnaryInvoker, options ...grpc.CallOption) error {
	trace, _ := ctx.Value(sdkDispatchContextKey{}).(*sdkDispatchTrace)
	if trace != nil {
		trace.entered.Store(true)
	}
	// The SDK's unary interceptor has already appended authentication. NewStream
	// invokes its stream interceptor, which appends that same authentication;
	// remove the first copy so the unchanged SDK supplies exactly one of each.
	headers, _ := metadata.FromOutgoingContext(ctx)
	headers = headers.Copy()
	headers.Delete(sdkAuthorizationHeader)
	headers.Delete(sdkProtocolHeader)
	ctx = metadata.NewOutgoingContext(ctx, headers)
	options = append(options, grpc.MaxRetryRPCBufferSize(0), grpc.WaitForReady(false),
		grpc.MaxCallSendMsgSize(64*1024), grpc.MaxCallRecvMsgSize(4*1024*1024))
	stream, err := conn.NewStream(ctx, &grpc.StreamDesc{}, method, options...)
	if err != nil {
		return err
	}
	_ = stream.Context()
	if trace != nil {
		// NewStream cannot send the unary request body. Once SendMsg is
		// entered, even its error remains ambiguous; never infer no effect.
		trace.body.Store(true)
	}
	if err := stream.SendMsg(request); err != nil && err != io.EOF {
		return err
	}
	if err := stream.CloseSend(); err != nil && err != io.EOF {
		return err
	}
	if err := stream.RecvMsg(reply); err != nil {
		return err
	}
	if err := stream.RecvMsg(reply); err != io.EOF {
		if err == nil {
			return status.Error(codes.Internal, messageMultipleReplies)
		}
		return err
	}
	return nil
}
