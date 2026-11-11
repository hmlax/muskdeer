package himalaya

import (
	"context"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSDKTracingPropagatesWithoutChangingSingleDispatchOrAuthentication(t *testing.T) {
	t.Parallel()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	peer := &sdkPeer{}
	client := sdkClient(t, peer, nil)
	ctx, root := provider.Tracer("test").Start(t.Context(), "quote")
	expected := root.SpanContext()
	if _, err := client.SubmitQuote(ctx, directOrder()); err != nil {
		t.Fatal(err)
	}
	root.End()
	peer.mu.Lock()
	defer peer.mu.Unlock()
	if peer.calls != 1 || peer.token != "Bearer sdk-test-token" || peer.protocol != "2" {
		t.Fatalf("SDK dispatch/auth changed: calls=%d protocol=%s", peer.calls, peer.protocol)
	}
	if !strings.HasPrefix(peer.traceparent, "00-"+expected.TraceID().String()+"-") {
		t.Fatal("single-dispatch SDK did not carry W3C trace context")
	}
	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("SDK spans=%d, want root and one upstream call", len(spans))
	}
	found := false
	for _, span := range spans {
		if span.Parent.SpanID() == expected.SpanID() {
			found = true
		}
	}
	if !found {
		t.Fatal("SDK call is not a child of the execution operation")
	}
}
