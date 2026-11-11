package tracing

import (
	"context"
	"net"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type traceService interface {
	Call(context.Context, *health.HealthCheckRequest) (*health.HealthCheckResponse, error)
}
type tracePeer struct{ next *grpc.ClientConn }

func (p *tracePeer) Call(ctx context.Context, in *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	if p.next != nil {
		out := new(health.HealthCheckResponse)
		err := p.next.Invoke(ctx, "/trace.Test/Call", in, out)
		return out, err
	}
	return nil, status.Error(grpcCodes.InvalidArgument, "private request contents must not be exported")
}
func servePeer(t *testing.T, provider trace.TracerProvider, peer *tracePeer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.StatsHandler(ServerHandler(provider)), grpc.ChainUnaryInterceptor(UnaryErrors))
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "trace.Test",
		HandlerType: (*traceService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Call",
			Handler: func(s any, ctx context.Context, decode func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
				in := new(health.HealthCheckRequest)
				if err := decode(in); err != nil {
					return nil, err
				}
				call := func(ctx context.Context, in any) (any, error) {
					return s.(traceService).Call(ctx, in.(*health.HealthCheckRequest))
				}
				return interceptor(ctx, in, &grpc.UnaryServerInfo{
					Server:     s,
					FullMethod: "/trace.Test/Call",
				}, call)
			},
		}},
	}, peer)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return listener.Addr().String()
}
func connectionFor(t *testing.T, address string, provider trace.TracerProvider) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(ClientHandler(provider)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Error(err)
		}
	})
	return conn
}

func TestTwoServicesRetainParentsAndRecordValidationErrors(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	first := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithResource(resource.Empty()))
	second := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithResource(resource.Empty()))
	t.Cleanup(func() {
		if err := first.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		if err := second.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	leaf := servePeer(t, second, &tracePeer{})
	middle := servePeer(t, first, &tracePeer{next: connectionFor(t, leaf, first)})
	conn := connectionFor(t, middle, first)
	ctx, root := first.Tracer("test").Start(t.Context(), "request")
	expected := root.SpanContext().TraceID()
	err := conn.Invoke(ctx, "/trace.Test/Call", &health.HealthCheckRequest{Service: "secret-password"}, new(health.HealthCheckResponse))
	root.End()
	if status.Code(err) != grpcCodes.InvalidArgument {
		t.Fatalf("Call error=%v, want InvalidArgument", err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 5 {
		t.Fatalf("exported spans=%d, want root plus two client/server pairs", len(spans))
	}
	ids := map[trace.SpanID]bool{}
	for _, span := range spans {
		ids[span.SpanContext.SpanID()] = true
	}
	for _, span := range spans {
		if span.SpanContext.TraceID() != expected {
			t.Error("cross-service trace identity was lost")
		}
		if span.Name != "request" && (!ids[span.Parent.SpanID()] || span.Status.Code != codes.Error) {
			t.Errorf("span %s parent/status=%v/%v, want a linked error", span.Name, span.Parent.SpanID(), span.Status)
		}
		if !span.EndTime.After(span.StartTime) {
			t.Error("span does not contain elapsed time")
		}
	}
}

func TestIntegerSamplingHonorsRemoteParent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.ParentBased(millionSampler(0))))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	_, root := provider.Tracer("test").Start(t.Context(), "unsampled")
	if root.IsRecording() {
		t.Error("zero ppm recorded a root")
	}
	root.End()
	carrier := mapCarrier("00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	ctx := Propagator().Extract(t.Context(), carrier)
	_, child := provider.Tracer("test").Start(ctx, "sampled-parent")
	if !child.IsRecording() {
		t.Error("zero root ppm dropped sampled remote parent")
	}
	child.End()
	if len(exporter.GetSpans()) != 1 {
		t.Fatal("expected exactly the sampled child")
	}
}

type mapCarrier string

func (c mapCarrier) Get(key string) string {
	if key == "traceparent" {
		return string(c)
	}
	return ""
}
func (mapCarrier) Set(string, string) {}
func (mapCarrier) Keys() []string     { return []string{"traceparent"} }

func TestTracingConfigurationRejectsInvalidEnabledSettings(t *testing.T) {
	valid := Config{
		Enabled:            true,
		Endpoint:           "127.0.0.1:4317",
		Insecure:           true,
		SamplingPerMillion: 1000000,
		ExportTimeout:      time.Second,
	}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Endpoint = "http://host:4317" }, func(c *Config) { c.SamplingPerMillion = 1000001 }, func(c *Config) { c.ExportTimeout = 0 }, func(c *Config) { c.RootCertFile = "private.pem" }} {
		cfg := valid
		change(&cfg)
		if Validate(cfg) == nil {
			t.Fatalf("Validate(%+v) accepted invalid configuration", cfg)
		}
	}
}
