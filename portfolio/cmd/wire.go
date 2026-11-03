//go:build wireinject

package main

import (
	"context"
	"io"

	"github.com/google/wire"

	"github.com/hmlax/muskdeer/deer"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/portfolio/internal/app"
)

func wireApp(ctx context.Context, cfg *configv1.ServiceConfig, output io.Writer, options []deer.Option) (*app.App, error) {
	wire.Build(app.ProviderSet)
	return nil, nil
}
