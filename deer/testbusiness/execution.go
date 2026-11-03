package testbusiness

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer/jetstream"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	executionbusiness "github.com/hmlax/muskdeer/execution/business"
)

func newExecution(ctx context.Context, db *gorm.DB, events *jetstream.Client, incarnation string, cfg *configv1.BusinessRuntime) (*Runtime, error) {
	service, err := executionbusiness.Assemble(ctx, db, events, incarnation, cfg)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		execution: service,
		workers:   service.Workers(),
	}, nil
}
