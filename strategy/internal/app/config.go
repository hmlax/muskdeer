// Package app owns the strategy process assembly and lifecycle.
package app

import (
	"context"
	"errors"

	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/business"
)

// Config is the private, validated startup snapshot supplied to this service graph.
type Config struct {
	service *configv1.ServiceConfig
}

// NewConfig validates service identity and snapshots the supplied Nacos content.
func NewConfig(ctx context.Context, cfg *configv1.ServiceConfig) (*Config, error) {
	if ctx == nil || cfg == nil || cfg.Name != "strategy" || cfg.Http != nil || cfg.AdminHttp != nil || cfg.Monitoring == nil || cfg.Grpc == nil ||
		cfg.GetBusiness().GetStrategy().GetManagementEnabled() || cfg.GetBusiness().GetStrategy().GetControlEnabled() {
		return nil, errors.New("strategy service requires its own gRPC configuration without administration")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot := proto.CloneOf(cfg)
	if err := business.ValidateConfig(snapshot); err != nil {
		return nil, err
	}
	return &Config{service: snapshot}, nil
}
