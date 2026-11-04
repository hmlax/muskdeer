// Package app owns the cron process assembly and lifecycle.
package app

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
)

// Config is the private, validated startup snapshot supplied to this service graph.
type Config struct {
	service *configv1.ServiceConfig
}

// NewConfig validates service identity and snapshots the supplied Nacos content.
func NewConfig(ctx context.Context, cfg *configv1.ServiceConfig) (*Config, error) {
	if ctx == nil || cfg == nil || cfg.Name != "cron" || cfg.Http != nil || cfg.AdminHttp != nil || cfg.Monitoring == nil || cfg.Grpc == nil ||
		cfg.GetBusiness().GetEnabled() {
		return nil, errors.New("cron requires its own gRPC configuration; scheduled jobs are not configured")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := proto.CloneOf(cfg)
	return &Config{service: snapshot}, nil
}
