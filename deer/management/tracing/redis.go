package tracing

import (
	"context"
	"errors"
	"net"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RedisHook records command names/times only, never keys, values or credentials.
type RedisHook struct{}

// DialHook preserves the existing connection behavior.
func (RedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}

// ProcessHook times commands under the caller trace; cache misses are normal.
func (RedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) (err error) {
		if !trace.SpanContextFromContext(ctx).IsValid() {
			return next(ctx, cmd)
		}
		ctx, span := StartChild(ctx, "redis."+cmd.Name(), trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system", "redis"), attribute.String("db.operation.name", cmd.Name())))
		defer func() {
			if !errors.Is(err, redis.Nil) {
				RecordError(ctx, err)
			}
			span.End()
		}()
		return next(ctx, cmd)
	}
}

// ProcessPipelineHook records one span for the complete pipeline.
func (RedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, commands []redis.Cmder) (err error) {
		if !trace.SpanContextFromContext(ctx).IsValid() {
			return next(ctx, commands)
		}
		ctx, span := StartChild(ctx, "redis.pipeline", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("db.system", "redis")))
		defer func() {
			if !errors.Is(err, redis.Nil) {
				RecordError(ctx, err)
			}
			span.End()
		}()
		return next(ctx, commands)
	}
}
