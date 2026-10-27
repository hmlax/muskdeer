package deer

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/requestid"
)

const requestMetadataFailure = "request metadata initialization failed"

type correlatedStream struct {
	grpc.ServerStream
	id requestid.ID
}

func requestUnaryInterceptor(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	id, err := incomingID(ctx, md.Get(requestid.Header))
	if err != nil {
		return nil, requestMetadataError(err)
	}
	ctx = requestid.Bind(ctx, id)
	if err := grpc.SetHeader(ctx, metadata.Pairs(requestid.Header, id.String())); err != nil {
		return nil, err
	}
	return handler(ctx, request)
}

func requestStreamInterceptor(service any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	id, err := incomingID(stream.Context(), md.Get(requestid.Header))
	if err != nil {
		return requestMetadataError(err)
	}
	if err := stream.SetHeader(metadata.Pairs(requestid.Header, id.String())); err != nil {
		return err
	}
	return handler(service, &correlatedStream{
		ServerStream: stream,
		id:           id,
	})
}

func (s *correlatedStream) Context() context.Context {
	// Delegate native lifetime on each call; no request context is stored here.
	return requestid.Bind(s.ServerStream.Context(), s.id)
}

func incomingID(ctx context.Context, values []string) (requestid.ID, error) {
	var candidate string
	if len(values) == 1 {
		candidate = values[0]
	}
	return requestid.New(ctx, candidate)
}

func requestMetadataError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return status.Error(codes.Internal, requestMetadataFailure)
}
