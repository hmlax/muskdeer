package tracing

import (
	"context"
	"net/http"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMiddleware starts before authentication, so denied requests have a trace.
// Only W3C headers and route/status are used; no bodies, cookies or query values.
func HTTPMiddleware(provider trace.TracerProvider) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		carrier := propagation.MapCarrier{}
		carrier.Set("traceparent", string(c.Request.Header.Peek("traceparent")))
		carrier.Set("tracestate", string(c.Request.Header.Peek("tracestate")))
		ctx = Propagator().Extract(ctx, carrier)
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := string(c.Method())
		ctx, span := provider.Tracer("muskdeer.http").Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.request.method", method), attribute.String("http.route", route)))
		defer func() {
			if value := recover(); value != nil {
				span.SetAttributes(attribute.Int("http.response.status_code", http.StatusInternalServerError))
				span.SetStatus(codes.Error, "HTTP 500")
				span.End()
				panic(value)
			}
			code := c.Response.StatusCode()
			span.SetAttributes(attribute.Int("http.response.status_code", code))
			if code >= http.StatusBadRequest {
				message := "HTTP " + strconv.Itoa(code)
				span.AddEvent("exception", trace.WithAttributes(attribute.String("exception.type", message)))
				span.SetStatus(codes.Error, message)
			}
			span.End()
		}()
		if span.SpanContext().IsValid() {
			c.Header("X-Trace-ID", span.SpanContext().TraceID().String())
		}
		c.Next(ctx)
	}
}
