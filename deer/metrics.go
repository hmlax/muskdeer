package deer

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hmlax/muskdeer/deer/metrics"
)

// Metrics returns this instance's fixed-cardinality registry. It has no export
// queue or global registration; callers must not put identifiers in metric labels.
func (d *Deer) Metrics() *metrics.Registry { return d.metrics }

func (d *Deer) measureUnary(ctx context.Context, request any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (reply any, err error) {
	started := time.Now()
	completed := false
	defer func() {
		code := codes.Internal
		if completed {
			code = metricGRPCCode(err)
		}
		d.metrics.ObserveGRPC(code, time.Since(started))
	}()
	reply, err = handler(ctx, request)
	completed = true
	return reply, err
}

func (d *Deer) measureStream(service any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	started := time.Now()
	completed := false
	defer func() {
		code := codes.Internal
		if completed {
			code = metricGRPCCode(err)
		}
		d.metrics.ObserveGRPC(code, time.Since(started))
	}()
	err = handler(service, stream)
	completed = true
	return err
}

func metricGRPCCode(err error) codes.Code {
	if errors.Is(err, context.DeadlineExceeded) {
		return codes.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		return codes.Canceled
	}
	return status.Code(err)
}

func (d *Deer) metricsExposition() []byte {
	output := d.metrics.Render()
	output = append(output, "# HELP deer_live Whether Run still owns the process.\n# TYPE deer_live gauge\ndeer_live "...)
	output = appendGauge(output, d.Live())
	output = append(output, "# HELP deer_lifecycle_ready Whether startup completed and drain has not begun.\n# TYPE deer_lifecycle_ready gauge\ndeer_lifecycle_ready "...)
	return appendGauge(output, d.Ready())
}

func appendGauge(output []byte, value bool) []byte {
	if value {
		return append(output, "1\n"...)
	}
	return append(output, "0\n"...)
}
