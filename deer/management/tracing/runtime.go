// Package tracing owns a process's bounded OTLP exporter and W3C propagation.
package tracing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Config contains infrastructure settings from Nacos, never OTEL environment defaults.
type Config struct {
	Enabled            bool          `yaml:"enabled"`
	Endpoint           string        `yaml:"endpoint"`
	Insecure           bool          `yaml:"insecure"`
	RootCertFile       string        `yaml:"root_cert_file"`
	SamplingPerMillion uint32        `yaml:"sampling_per_million"`
	ExportTimeout      time.Duration `yaml:"export_timeout"`
}

// Identity fixes the process resource independently of request headers or env.
type Identity struct {
	Service  string
	Instance string
	Version  string
}

// Runtime owns its provider/export connection. It never installs process globals.
type Runtime struct {
	provider trace.TracerProvider
	shutdown func(context.Context) error
	once     sync.Once
	closeErr error
}

// Validate rejects malformed enabled exporter settings before serving.
func Validate(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	host, port, err := net.SplitHostPort(cfg.Endpoint)
	n, e := strconv.ParseUint(port, 10, 16)
	if err != nil || host == "" || e != nil || n == 0 || cfg.SamplingPerMillion > 1000000 || cfg.ExportTimeout <= 0 || cfg.ExportTimeout > 30*time.Second || cfg.Insecure && cfg.RootCertFile != "" {
		return errors.New("invalid tracing endpoint, sampling, TLS or export timeout")
	}
	return nil
}

// New creates one asynchronous provider with a bounded nonblocking export queue.
func New(ctx context.Context, cfg Config, id Identity) (*Runtime, error) {
	if ctx == nil {
		return nil, errors.New("tracing context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := Validate(cfg); err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return &Runtime{provider: noop.NewTracerProvider()}, nil
	}
	transport := credentials.TransportCredentials(insecure.NewCredentials())
	if !cfg.Insecure {
		security := &tls.Config{MinVersion: tls.VersionTLS12}
		if cfg.RootCertFile != "" {
			pem, err := os.ReadFile(cfg.RootCertFile)
			if err != nil {
				return nil, errors.New("read tracing trust root failed")
			}
			security.RootCAs = x509.NewCertPool()
			if !security.RootCAs.AppendCertsFromPEM(pem) {
				return nil, errors.New("invalid tracing trust root")
			}
		}
		transport = credentials.NewTLS(security)
	}
	conn, err := grpc.NewClient("passthrough:///"+cfg.Endpoint, grpc.WithTransportCredentials(transport), grpc.WithNoProxy(), grpc.WithDisableRetry())
	if err != nil {
		return nil, errors.New("create tracing export connection failed")
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn), otlptracegrpc.WithTimeout(cfg.ExportTimeout), otlptracegrpc.WithHeaders(map[string]string{}), otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{Enabled: false}))
	if err != nil {
		return nil, errors.Join(errors.New("create tracing exporter failed"), conn.Close())
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", id.Service), attribute.String("service.instance.id", id.Instance), attribute.String("service.version", id.Version))),
		sdktrace.WithSampler(sdktrace.ParentBased(millionSampler(cfg.SamplingPerMillion))),
		sdktrace.WithBatcher(safeExporter{SpanExporter: exporter}, sdktrace.WithMaxQueueSize(2048), sdktrace.WithMaxExportBatchSize(256), sdktrace.WithBatchTimeout(time.Second), sdktrace.WithExportTimeout(cfg.ExportTimeout)),
	)
	return &Runtime{
		provider: provider,
		shutdown: func(ctx context.Context) error { return errors.Join(provider.Shutdown(ctx), conn.Close()) },
	}, nil
}

// Provider returns the borrowed provider; an absent runtime is a no-op.
func (r *Runtime) Provider() trace.TracerProvider {
	if r == nil {
		return noop.NewTracerProvider()
	}
	return r.provider
}

// Close flushes ended spans before closing the owned connection, at most once.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil || r.shutdown == nil {
		return nil
	}
	r.once.Do(func() { r.closeErr = r.shutdown(ctx) })
	return r.closeErr
}

// ParentBased honors the caller's W3C sampling decision; roots use integer ppm.
type millionSampler uint32

func (s millionSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	decision := sdktrace.Drop
	if binary.BigEndian.Uint64(p.TraceID[:8])%1000000 < uint64(s) {
		decision = sdktrace.RecordAndSample
	}
	return sdktrace.SamplingResult{Decision: decision}
}
func (millionSampler) Description() string { return "IntegerPerMillion" }

// Propagator carries W3C Trace Context, without copying application baggage.
func Propagator() propagation.TextMapPropagator { return propagation.TraceContext{} }
