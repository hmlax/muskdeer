package tracing

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	health "google.golang.org/grpc/health/grpc_health_v1"
)

func TestHertzRequestRetainsW3CParentThroughGRPCAndReturnsTraceID(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	address := servePeer(t, provider, &tracePeer{})
	conn := connectionFor(t, address, provider)
	h := server.New()
	h.Use(HTTPMiddleware(provider))
	h.GET("/account/:id", func(ctx context.Context, c *app.RequestContext) {
		err := conn.Invoke(ctx, "/trace.Test/Call", &health.HealthCheckRequest{}, new(health.HealthCheckResponse))
		if err != nil {
			c.Status(400)
		}
	})
	result := ut.PerformRequest(h.Engine, "GET", "/account/123?password=private", nil, ut.Header{
		Key:   "traceparent",
		Value: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
	})
	if result.Code != 400 || result.Header().Get("X-Trace-ID") != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("HTTP status/trace=%d/%s", result.Code, result.Header().Get("X-Trace-ID"))
	}
	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("HTTP/gRPC spans=%d, want 3", len(spans))
	}
	for _, span := range spans {
		if span.SpanContext.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" || span.Status.Code != codes.Error {
			t.Errorf("broken HTTP/gRPC error trace: %s %+v", span.Name, span.Status)
		}
		if strings.Contains(span.Name, "123") || strings.Contains(span.Name, "private") {
			t.Errorf("span name contains raw input: %s", span.Name)
		}
	}
}
