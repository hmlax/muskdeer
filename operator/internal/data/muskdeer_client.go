package data

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"time"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/management/runtimeconfig"
	"github.com/hmlax/muskdeer/deer/management/tracing"
	pb "github.com/hmlax/muskdeer/deer/pb/strategy/console/v1"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const muskdeerDisabled = "Muskdeer connection is disabled"

// MuskdeerConnection owns only an authenticated core client, never a business DB.
type MuskdeerConnection struct {
	conn    *grpc.ClientConn
	timeout time.Duration
}

func NewMuskdeerConnection(ctx *bootstrap.Context) (*MuskdeerConnection, func(), error) {
	result, err := OpenMuskdeerConnection(runtimeconfig.SettingsFromContext(ctx).Muskdeer, runtimeconfig.TracingFromContext(ctx).Provider())
	if err != nil {
		return nil, nil, err
	}
	logger := ctx.NewLoggerHelper("muskdeer/client/operator")
	return result, func() {
		if result.conn != nil {
			if err := result.conn.Close(); err != nil {
				logger.Errorf(ctx.Context(), "close Muskdeer connection: %v", err)
			}
		}
	}, nil
}

// OpenMuskdeerConnection snapshots Nacos infrastructure settings; no insecure fallback.
func OpenMuskdeerConnection(cfg runtimeconfig.MuskdeerSettings, providers ...trace.TracerProvider) (*MuskdeerConnection, error) {
	if !cfg.Enabled {
		return &MuskdeerConnection{}, nil
	}
	if _, _, err := net.SplitHostPort(cfg.Endpoint); err != nil || cfg.ServerName != "strategy.grpc" || cfg.TimeoutMs < 100 || cfg.TimeoutMs > 30000 {
		return nil, errors.New("invalid Muskdeer endpoint, identity or timeout")
	}
	roots, err := os.ReadFile(cfg.RootCertFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(roots) {
		return nil, errors.New("invalid Muskdeer trust root")
	}
	cert, err := tls.LoadX509KeyPair(cfg.CertificateFile, cfg.PrivateKeyFile)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	if !slices.Contains(leaf.DNSNames, "operator.grpc") {
		return nil, errors.New("muskdeer client certificate requires operator.grpc identity")
	}
	provider := trace.TracerProvider(noop.NewTracerProvider())
	if len(providers) > 0 && providers[0] != nil {
		provider = providers[0]
	}
	conn, err := grpc.NewClient("passthrough:///"+cfg.Endpoint,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:   tls.VersionTLS13,
			ServerName:   cfg.ServerName,
			RootCAs:      pool,
			Certificates: []tls.Certificate{cert},
		})),
		grpc.WithNoProxy(), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(),
		grpc.WithStatsHandler(tracing.ClientHandler(provider)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(16<<20), grpc.MaxCallSendMsgSize(16<<20)),
	)
	if err != nil {
		return nil, err
	}
	return &MuskdeerConnection{
		conn:    conn,
		timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond,
	}, nil
}

func (c *MuskdeerConnection) Client() pb.ConsoleClient { return pb.NewConsoleClient(c) }

// Invoke commits one unary stream before sending its body, preventing transparent
// replay of control writes as well as configured retries. A timeout needs refresh.
func (c *MuskdeerConnection) Invoke(ctx context.Context, method string, request, reply any, options ...grpc.CallOption) error {
	if c.conn == nil {
		return status.Error(codes.Unavailable, muskdeerDisabled)
	}
	bounded, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	stream, err := c.conn.NewStream(bounded, &grpc.StreamDesc{}, method, options...)
	if err != nil {
		return err
	}
	_ = stream.Context()
	if err := stream.SendMsg(request); err != nil && err != io.EOF {
		return err
	}
	if err := stream.CloseSend(); err != nil && err != io.EOF {
		return err
	}
	if err := stream.RecvMsg(reply); err != nil {
		return err
	}
	if err := stream.RecvMsg(reply); err != io.EOF {
		if err == nil {
			return status.Error(codes.Internal, "multiple unary responses")
		}
		return err
	}
	return nil
}
func (c *MuskdeerConnection) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, "Muskdeer administration uses unary RPCs")
}
