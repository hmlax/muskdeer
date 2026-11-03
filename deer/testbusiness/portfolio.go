package testbusiness

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/jetstream"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	portfoliobusiness "github.com/hmlax/muskdeer/portfolio/business"
)

func newPortfolio(ctx context.Context, db *gorm.DB, owner *deer.Deer, events *jetstream.Client, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	service, err := portfoliobusiness.Assemble(ctx, db, owner, events, cfg)
	if err != nil {
		return nil, err
	}
	return &Runtime{workers: service.Workers()}, nil
}
