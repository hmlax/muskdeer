//go:build integration

package infrastructureprobe

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/redis"
)

func TestDeerInjectedDatabaseAndRedisOwnership(t *testing.T) {
	database := openClient(t, postgresConfig(t), "borrowed-database")
	cache := openCache(t, redisConfig(), "borrowed-cache")
	cfg := serviceConfig(t, "injected-dependencies")
	cfg.Redis = redisConfig()
	// These credentials cannot open replacements. The injected clients must win.
	const unusedSecret = "MUSKDEER_TEST_UNUSED_OPTION_PASSWORD"
	t.Setenv(unusedSecret, "")
	cfg.Postgres.PasswordEnv = unusedSecret
	cfg.Redis.PasswordEnv = unusedSecret
	options := []deer.Option{deer.WithDB(database), deer.WithRedis(cache)}
	failure := errors.New("business registration rejected")
	_, _, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{
		LogOutput: io.Discard,
		RegisterGRPC: func(_ context.Context, owner *deer.Deer, _ grpc.ServiceRegistrar) error {
			db, err := owner.DB()
			if err != nil || db != database.GORM() {
				t.Fatalf("injected database = %p, %v; want original GORM handle", db, err)
			}
			redis, err := owner.Redis()
			if err != nil || redis != cache {
				t.Fatalf("injected cache = %p, %v; want original Redis client", redis, err)
			}
			return failure
		},
	}, options...)
	if !errors.Is(err, failure) {
		t.Fatalf("injected constructor rollback = %v; want registration failure", err)
	}
	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("rollback closed borrowed database: %v", err)
	}
	if err := cache.Ping(t.Context()); err != nil {
		t.Fatalf("rollback closed borrowed Redis: %v", err)
	}
	owner, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{LogOutput: io.Discard}, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- owner.Run(t.Context()) }()
	ready, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := owner.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if err := owner.CheckReady(ready); err != nil {
		t.Fatal(err)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("shutdown closed borrowed database: %v", err)
	}
	if err := cache.Set(t.Context(), "injection:after-shutdown", "0.01", time.Second); err != nil {
		t.Fatalf("shutdown closed borrowed Redis: %v", err)
	}
	assertInjectedRedisReadiness(t, cfg, options, cache)
	if _, _, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{LogOutput: io.Discard}, options...); err == nil {
		t.Fatal("constructor accepted a closed injected Redis client")
	}
	if err := database.Ping(t.Context()); err != nil {
		t.Fatalf("dependency check rollback closed borrowed database: %v", err)
	}
}

func assertInjectedRedisReadiness(t *testing.T, cfg *configv1.ServiceConfig, options []deer.Option, cache *redis.Client) {
	t.Helper()
	owner, cleanup, err := deer.NewDeer(t.Context(), cfg, deer.ServiceSpec{LogOutput: io.Discard}, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := cleanup(t.Context()); err != nil {
			t.Error(err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- owner.Run(t.Context()) }()
	ready, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := owner.WaitReady(ready); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.CheckReady(ready); err == nil {
		t.Fatal("readiness ignored a closed injected Redis client")
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
