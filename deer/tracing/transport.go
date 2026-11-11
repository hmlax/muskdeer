package tracing

import (
	"context"
	"errors"
	"io"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
	"google.golang.org/grpc/status"
)

// ServerHandler traces gRPC requests, excluding routine health polling.
func ServerHandler(provider trace.TracerProvider) stats.Handler {
	return otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(provider), otelgrpc.WithPropagators(Propagator()), otelgrpc.WithFilter(rpcFilter))
}

// ClientHandler instruments actual gRPC streams, including single-send unary calls.
func ClientHandler(provider trace.TracerProvider) stats.Handler {
	return otelgrpc.NewClientHandler(otelgrpc.WithTracerProvider(provider), otelgrpc.WithPropagators(Propagator()), otelgrpc.WithFilter(rpcFilter))
}
func rpcFilter(info *stats.RPCTagInfo) bool {
	return !strings.HasPrefix(info.FullMethodName, "/grpc.health.v1.Health/")
}

// RecordError retains the status/type, without exporting response bodies or secrets.
func RecordError(ctx context.Context, err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	message := status.Code(err).String()
	span.RecordError(errors.New(message))
	span.SetStatus(codes.Error, message)
}

// UnaryErrors also marks domain/validation failures, which gRPC semantic
// conventions otherwise consider a successful server transport.
func UnaryErrors(ctx context.Context, request any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (reply any, err error) {
	defer func() { RecordError(ctx, err) }()
	return next(ctx, request)
}

// StartChild uses the current process span's provider, preserving resource identity.
// No parent means no implicit exporter or global provider is created.
func StartChild(ctx context.Context, name string, options ...trace.SpanStartOption) (context.Context, trace.Span) {
	return trace.SpanFromContext(ctx).TracerProvider().Tracer("muskdeer").Start(ctx, name, options...)
}
