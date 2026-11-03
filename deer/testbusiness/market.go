package testbusiness

import (
	"context"

	"gorm.io/gorm"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	marketbusiness "github.com/hmlax/muskdeer/market/business"
)

func newMarket(ctx context.Context, db *gorm.DB, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	service, err := marketbusiness.Assemble(ctx, db, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		market:  service,
		workers: service.Workers(),
	}, nil
}
