package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/hmlax/muskdeer/operator/internal/data"
)

// Exercise the production injector rather than a separate test provider graph.
// Both paths create the script listener; its Redis connection must be released
// when the application closes or when a later transport provider fails.
func TestWireApplicationLifecycle(t *testing.T) {
	for _, test := range []struct {
		name       string
		grpcConfig *conf.Server_GRPC
		wantError  bool
	}{
		{
			name: "application cleanup",
			grpcConfig: &conf.Server_GRPC{
				Addr:    "127.0.0.1:0",
				Timeout: durationpb.New(time.Second),
			},
		},
		{
			name:      "transport failure cleanup",
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			redis := miniredis.RunT(t)
			config := &conf.Bootstrap{
				Data: &conf.Data{
					Database: &conf.Data_Database{
						Driver: "sqlite",
						Source: filepath.Join(t.TempDir(), "operator.db"),
					},
					Redis: &conf.Data_Redis{
						Network: "tcp",
						Addr:    redis.Addr(),
					},
				},
				Oss: &conf.OSS{
					Minio: &conf.OSS_MinIO{Endpoint: "127.0.0.1:9000"},
				},
				Server: &conf.Server{Grpc: test.grpcConfig},
			}
			ctx := bootstrap.NewContextWithParam(context.Background(), nil, config, bLogger.NopLogger())
			db, closeDB, err := data.NewGormClient(ctx)
			require.NoError(t, err)
			err = db.Client().Schema.Create(context.Background())
			closeDB()
			require.NoError(t, err)

			app, cleanup, err := initApp(ctx)
			if test.wantError {
				require.ErrorContains(t, err, "operator gRPC config is required")
				require.Nil(t, app)
				require.Nil(t, cleanup)
			} else {
				require.NoError(t, err)
				require.NotNil(t, app)
				require.NotNil(t, cleanup)
				cleanup()
			}
			require.Eventually(t, func() bool {
				return redis.CurrentConnectionCount() == 0
			}, time.Second, 10*time.Millisecond, "injector leaked a Redis connection")
		})
	}
}
