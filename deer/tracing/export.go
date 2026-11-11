package tracing

import (
	"context"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Libraries can attach driver error text to spans. Sanitize at the export
// boundary too, so SQL values/credentials cannot escape through exception text.
type safeExporter struct{ sdktrace.SpanExporter }

func (e safeExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	safe := make([]sdktrace.ReadOnlySpan, len(spans))
	for i, span := range spans {
		safe[i] = safeSpan{ReadOnlySpan: span}
	}
	return e.SpanExporter.ExportSpans(ctx, safe)
}

type safeSpan struct{ sdktrace.ReadOnlySpan }

func (s safeSpan) Status() sdktrace.Status {
	value := s.ReadOnlySpan.Status()
	if value.Code == codes.Error {
		value.Description = "operation failed"
	}
	return value
}
func (s safeSpan) Events() []sdktrace.Event {
	events := slices.Clone(s.ReadOnlySpan.Events())
	for i, event := range events {
		events[i].Attributes = safeAttributes(event.Attributes)
	}
	return events
}
func (s safeSpan) Attributes() []attribute.KeyValue {
	return safeAttributes(s.ReadOnlySpan.Attributes())
}
func safeAttributes(attributes []attribute.KeyValue) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(attributes))
	for _, value := range attributes {
		key := string(value.Key)
		if key == "exception.message" || key == "exception.stacktrace" {
			value.Value = attribute.StringValue("operation failed")
		}
		if key == "db.statement" || key == "db.query.text" || key == "db.connection_string" || strings.Contains(key, "password") || strings.Contains(key, "authorization") || strings.Contains(key, "cookie") {
			continue
		}
		out = append(out, value)
	}
	return out
}
