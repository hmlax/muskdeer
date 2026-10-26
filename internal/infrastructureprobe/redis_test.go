//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	goredis "github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/redis"
)

func TestRedisInfrastructure(t *testing.T) {
	if os.Getenv("MUSKDEER_TEST_REDIS_PASSWORD") == "" {
		t.Fatal("isolated Redis credentials are required; run make integration")
	}
	t.Run("exact values namespaces and TTL", testCacheValues)
	t.Run("deadline bounds a paused server command", testCacheDeadline)
	t.Run("exhausted pool waits within caller budget", testCachePoolLimit)
	t.Run("deer dependencies and readiness", testCacheReadiness)
	t.Run("API failure closes shared cache", testCacheAPIRollback)
	t.Run("authentication error preserves safe cause", testCacheAuthentication)
}

func testCacheValues(t *testing.T) {
	const key = "quote:BTC-USDT"
	const exact = "12345678901234567890.123456789012345678"
	client := openCache(t, redisConfig(), "values")
	if err := client.Set(t.Context(), key, exact, time.Second); err != nil {
		t.Fatal(err)
	}
	peer := openCache(t, redisConfig(), "values-peer")
	if value, err := peer.Get(t.Context(), key); err != nil || value != exact {
		t.Fatalf("same namespace Get = %q, %v", value, err)
	}
	isolatedConfig := redisConfig()
	isolatedConfig.KeyPrefix = "integration:other-service"
	isolated := openCache(t, isolatedConfig, "values-isolated")
	if _, err := isolated.Get(t.Context(), key); !errors.Is(err, redis.ErrNotFound) {
		t.Fatalf("other namespace Get = %v", err)
	}
	admin := redisAdmin(t)
	ttl, err := admin.PTTL(t.Context(), redisConfig().KeyPrefix+":"+key).Result()
	if err != nil || ttl <= 0 || ttl > time.Second {
		t.Fatalf("server TTL = %s, %v", ttl, err)
	}
	for _, ttl := range []time.Duration{0, -time.Second, time.Microsecond, time.Millisecond + time.Nanosecond} {
		if err := client.Set(t.Context(), "invalid:ttl", exact, ttl); err == nil {
			t.Fatalf("invalid TTL %s accepted", ttl)
		}
	}
	if err := client.Set(t.Context(), "", exact, time.Second); err == nil {
		t.Fatal("empty resource key accepted")
	}
	if count, err := admin.Exists(t.Context(), redisConfig().KeyPrefix+":invalid:ttl").Result(); err != nil || count != 0 {
		t.Fatalf("invalid TTL write reached Redis: %d, %v", count, err)
	}
	if err := client.Delete(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Get(t.Context(), key); !errors.Is(err, redis.ErrNotFound) {
		t.Fatalf("deleted Get = %v", err)
	}
	if err := client.Set(t.Context(), "expires:soon", exact, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	expiry, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ticks := time.NewTicker(5 * time.Millisecond)
	defer ticks.Stop()
	for {
		_, err := client.Get(expiry, "expires:soon")
		if errors.Is(err, redis.ErrNotFound) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-expiry.Done():
			t.Fatal("server did not expire cache entry within budget")
		case <-ticks.C:
		}
	}
	for range 2 {
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.Set(t.Context(), key, exact, time.Second); err == nil {
		t.Fatal("closed cache reopened")
	}
}

func testCacheDeadline(t *testing.T) {
	cfg := redisConfig()
	cfg.PoolSize = 1
	client := openCache(t, cfg, "deadline")
	admin := redisAdmin(t)
	// This admin connection can affect only the dedicated temporary test server.
	if err := admin.ClientPause(t.Context(), 400*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer cancel()
		if err := admin.ClientUnpause(ctx).Err(); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.Set(ctx, "deadline:ambiguous", "0.1", time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paused SET = %v; want caller deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("command exceeded caller budget: %s", elapsed)
	}
	if stats := client.Stats(); stats.TotalConns > 1 {
		t.Fatalf("cache pool exceeded hard connection bound: %+v", stats)
	}
	// A timed-out SET is ambiguous. Do not assert it was absent or retry it.
}

func testCachePoolLimit(t *testing.T) {
	cfg := redisConfig()
	cfg.PoolSize = 1
	client := openCache(t, cfg, "pool-limit")
	admin := redisAdmin(t)
	if err := admin.ClientPause(t.Context(), 400*time.Millisecond).Err(); err != nil {
		t.Fatal(err)
	}
	first, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	result := make(chan error, 1)
	go func() { result <- client.Ping(first) }()
	defer func() {
		defer cancel()
		if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("paused pool holder = %v", err)
		}
		ctx, stop := context.WithTimeout(context.WithoutCancel(t.Context()), time.Second)
		defer stop()
		if err := admin.ClientUnpause(ctx).Err(); err != nil {
			t.Error(err)
		}
	}()
	ticks := time.NewTicker(time.Millisecond)
	defer ticks.Stop()
	for client.Stats().IdleConns != 0 {
		select {
		case <-ticks.C:
		case <-first.Done():
			t.Fatal("first request never acquired the only cache connection")
		}
	}
	second, stop := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer stop()
	started := time.Now()
	if err := client.Ping(second); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exhausted pool Ping = %v; want deadline", err)
	}
	// SDK WaitCount counts successful acquisitions after waiting, excluding
	// canceled waiters. Assert the observable wait and retained hard bound.
	if elapsed := time.Since(started); elapsed < 25*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Fatalf("exhausted pool wait = %s; want caller budget", elapsed)
	}
	if stats := client.Stats(); stats.TotalConns != 1 || stats.IdleConns != 0 {
		t.Fatalf("hard pool bound/wait = %+v", stats)
	}
}

func testCacheReadiness(t *testing.T) {
	cfg := serviceConfig("cache-ready")
	cfg.Postgres = nil
	cfg.Redis = redisConfig()
	var cache *redis.Client
	service, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(ctx context.Context, owner *deer.Deer, _ grpc.ServiceRegistrar) error {
			var err error
			cache, err = owner.Redis()
			if err != nil {
				return err
			}
			return cache.Set(ctx, "startup:probe", "0.01", time.Second)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- service.Run(t.Context()) }()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := service.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := service.CheckReady(ctx); err == nil {
		t.Fatal("readiness ignored unavailable Redis")
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func testCacheAPIRollback(t *testing.T) {
	cfg := serviceConfig("api-cache-rollback")
	cfg.Redis = redisConfig()
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	failure := errors.New("route assembly rejected")
	var cache *redis.Client
	api, cleanup, err := deer.NewDeerApi(t.Context(), cfg, deer.APISpec{
		Service: deer.ServiceSpec{
			LogOutput: io.Discard,
		},
		Routes: func(_ context.Context, owner *deer.Deer, _ *server.Hertz) error {
			var err error
			cache, err = owner.Redis()
			if err != nil {
				return err
			}
			return failure
		},
	})
	if api != nil || cleanup != nil || !errors.Is(err, failure) {
		t.Fatalf("API initialization = %v, cleanup nil %v, %v", api, cleanup == nil, err)
	}
	if cache == nil || cache.Ping(t.Context()) == nil || cache.Stats().TotalConns != 0 {
		t.Fatal("API failure did not release shared Redis")
	}
}

func testCacheAuthentication(t *testing.T) {
	const secret = "invalid_redis_test_only"
	const reference = "MUSKDEER_TEST_INVALID_REDIS_PASSWORD"
	t.Setenv(reference, secret)
	cfg := redisConfig()
	cfg.PasswordEnv = reference
	client, err := redis.New(t.Context(), cfg, "invalid-auth")
	var serverError goredis.Error
	if client != nil || !errors.As(err, &serverError) || !strings.HasPrefix(serverError.Error(), "WRONGPASS") {
		t.Fatalf("authentication rejection = %v, %v", client, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("authentication error leaked password: %v", err)
	}
}

func openCache(t *testing.T, cfg *configv1.Redis, name string) *redis.Client {
	t.Helper()
	client, err := redis.New(t.Context(), cfg, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func redisAdmin(t *testing.T) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:                  redisConfig().Address,
		Password:              os.Getenv("MUSKDEER_TEST_REDIS_PASSWORD"),
		Protocol:              2,
		PoolSize:              1,
		MaxActiveConns:        1,
		PipelinePoolSize:      -1,
		MaxRetries:            -1,
		DialerRetries:         1,
		DialTimeout:           time.Second,
		ReadTimeout:           time.Second,
		WriteTimeout:          time.Second,
		ContextTimeoutEnabled: true,
		DisableIdentity:       true,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func redisConfig() *configv1.Redis {
	return &configv1.Redis{
		Address:        "127.0.0.1:55440",
		PasswordEnv:    "MUSKDEER_TEST_REDIS_PASSWORD",
		KeyPrefix:      "integration:cache",
		PoolSize:       2,
		ConnectTimeout: durationpb.New(time.Second),
		CommandTimeout: durationpb.New(time.Second),
		PoolTimeout:    durationpb.New(100 * time.Millisecond),
		MaxIdleTime:    durationpb.New(30 * time.Second),
		MaxLifetime:    durationpb.New(time.Minute),
		TlsEnabled:     proto.Bool(false),
	}
}
