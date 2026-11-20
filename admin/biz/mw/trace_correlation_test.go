package mw

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestAuditTraceIDUsesActiveSpanWithoutBrowserHeaders(t *testing.T) {
	id, err := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := trace.SpanIDFromHex("b7ad6b7169203331")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: id,
		SpanID:  spanID,
	}))
	if got := TraceIDFromContext(ctx); got != id.String() {
		t.Fatalf("audit trace ID=%q, want active span ID %q", got, id.String())
	}
}
