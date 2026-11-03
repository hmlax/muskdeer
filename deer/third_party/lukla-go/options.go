package orderclient

import (
	"os"
	"time"

	"google.golang.org/grpc"
)

type config struct {
	authToken   string
	timeout     time.Duration
	dialOptions []grpc.DialOption
}

func defaultConfig() config {
	return config{
		timeout:   30 * time.Second,
		authToken: os.Getenv("CLIENT_AUTH_TOKEN"),
	}
}

// Option configures the order-service client.
type Option func(*config)

// WithTimeout sets the default deadline for unary RPCs when the context has none.
func WithTimeout(d time.Duration) Option {
	return func(c *config) {
		c.timeout = d
	}
}

// WithDialOptions appends gRPC dial options (TLS, keepalive, etc.).
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c *config) {
		c.dialOptions = append(c.dialOptions, opts...)
	}
}

// WithAuthToken supplies a service token, or a separate administrator token for funding RPCs.
func WithAuthToken(token string) Option {
	return func(c *config) { c.authToken = token }
}
