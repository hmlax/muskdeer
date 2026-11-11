package tracing

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

type traceCollector struct {
	collector.UnimplementedTraceServiceServer
	mu    sync.Mutex
	spans []*tracepb.ResourceSpans
}

func (c *traceCollector) Export(_ context.Context, in *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, in.ResourceSpans...)
	return &collector.ExportTraceServiceResponse{}, nil
}
func TestConfiguredOTLPTargetAndShutdownFlushOverrideEnvironment(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://unconfigured.invalid:1")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	t.Setenv("OTEL_SERVICE_NAME", "wrong-service")
	receiver := &traceCollector{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collector.RegisterTraceServiceServer(server, receiver)
	joined := make(chan error, 1)
	go func() { joined <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-joined; err != nil {
			t.Error(err)
		}
	}()
	runtime, err := New(t.Context(), Config{
		Enabled:            true,
		Endpoint:           listener.Addr().String(),
		Insecure:           true,
		SamplingPerMillion: 1000000,
		ExportTimeout:      time.Second,
	}, Identity{
		Service:  "configured-service",
		Instance: "one",
		Version:  "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, span := runtime.Provider().Tracer("test").Start(t.Context(), "queued")
	span.RecordError(errors.New("password=private"))
	span.SetStatus(codes.Error, "private driver error")
	span.SetAttributes(attribute.String("db.statement", "select private"))
	span.End()
	shutdown, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := runtime.Close(shutdown); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(shutdown); err != nil {
		t.Fatal("repeated tracing cleanup", err)
	}
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	found := false
	for _, r := range receiver.spans {
		for _, scope := range r.ScopeSpans {
			for _, span := range scope.Spans {
				if strings.Contains(span.GetStatus().GetMessage(), "private") {
					t.Fatal("export leaked raw failure text")
				}
				for _, attr := range span.Attributes {
					if attr.Key == "db.statement" {
						t.Fatal("export leaked SQL text")
					}
				}
				for _, event := range span.Events {
					for _, attr := range event.Attributes {
						if strings.Contains(attr.Value.GetStringValue(), "private") {
							t.Fatal("export leaked exception text")
						}
					}
				}
			}
		}
		for _, a := range r.Resource.Attributes {
			if a.Key == "service.name" && a.Value.GetStringValue() == "configured-service" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("shutdown failed to export the configured service to the configured target")
	}
}

type blockingCollector struct {
	collector.UnimplementedTraceServiceServer
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *blockingCollector) Export(ctx context.Context, _ *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	c.once.Do(func() { close(c.started) })
	select {
	case <-c.release:
		return &collector.ExportTraceServiceResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestFullExportQueueDoesNotBlockBusinessSpanCompletion(t *testing.T) {
	receiver := &blockingCollector{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	collector.RegisterTraceServiceServer(server, receiver)
	joined := make(chan error, 1)
	go func() { joined <- server.Serve(listener) }()
	defer func() {
		server.Stop()
		if err := <-joined; err != nil {
			t.Error(err)
		}
	}()
	runtime, err := New(t.Context(), Config{
		Enabled:            true,
		Endpoint:           listener.Addr().String(),
		Insecure:           true,
		SamplingPerMillion: 1000000,
		ExportTimeout:      3 * time.Second,
	}, Identity{Service: "bounded-export-test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		close(receiver.release)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	tracer := runtime.Provider().Tracer("test")
	for range 256 {
		_, span := tracer.Start(t.Context(), "prime")
		span.End()
	}
	limit, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	select {
	case <-receiver.started:
	case <-limit.Done():
		t.Fatal("collector never received the first batch")
	}
	complete := make(chan struct{})
	go func() {
		for range 5000 {
			_, span := tracer.Start(t.Context(), "over-capacity")
			span.End()
		}
		close(complete)
	}()
	select {
	case <-complete:
	case <-limit.Done():
		t.Fatal("business span completion blocked behind telemetry export")
	}
}
