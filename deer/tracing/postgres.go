package tracing

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// PostgresTracer records operations only: no SQL, arguments, DSN or returned data.
type PostgresTracer struct{}

// TraceQueryStart creates a child span without retaining query text or arguments.
func (PostgresTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx
	}
	operation := "QUERY"
	if fields := strings.Fields(data.SQL); len(fields) > 0 {
		switch strings.ToUpper(fields[0]) {
		case "SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK":
			operation = strings.ToUpper(fields[0])
		}
	}
	ctx, _ = StartChild(ctx, "postgres."+operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system", "postgresql"), attribute.String("db.operation.name", operation)))
	return ctx
}

// TraceQueryEnd records failure status and closes the query span.
func (PostgresTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return
	}
	RecordError(ctx, data.Err)
	trace.SpanFromContext(ctx).End()
}
