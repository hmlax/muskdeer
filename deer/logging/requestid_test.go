package logging

import (
	"bytes"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/hmlax/muskdeer/deer/requestid"
)

func TestRequestIDRemainsProtectedAtRootWithScopedLogger(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger, err := New(t.Context(), nil, Identity{
		Service:  "api",
		Instance: "correlation-test",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	id, err := requestid.New(t.Context(), "edge-correlation-id")
	if err != nil {
		t.Fatal(err)
	}
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	ctx := trace.ContextWithSpanContext(requestid.Bind(t.Context(), id), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
	logger.Slog().With("request_id", "spoofed", "trace_id", "spoofed", "span_id", "spoofed").WithGroup("action").With("result", "accepted").InfoContext(ctx, "request observed")
	var record struct {
		RequestID string `json:"request_id"`
		TraceID   string `json:"trace_id"`
		SpanID    string `json:"span_id"`
		Service   string `json:"service"`
		Action    struct {
			Result string `json:"result"`
		} `json:"action"`
	}
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.TraceID != traceID.String() || record.SpanID != spanID.String() || record.RequestID != id.String() || record.Service != "api" || record.Action.Result != "accepted" {
		t.Fatalf("scoped request correlation = %+v", record)
	}
}
