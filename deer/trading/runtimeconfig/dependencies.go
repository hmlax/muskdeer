package runtimeconfig

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate"
	himalayamigrate "github.com/hmlax/muskdeer/deer/migrate/himalaya"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/proto/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/native"
)

// Data borrows one owned pool and verifies the existing migration ledger.
// It does not select, construct or run a business service.
func Data(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*gorm.DB, string, error) {
	if ctx == nil || cfg == nil || owner == nil || !cfg.GetBusiness().GetEnabled() {
		return nil, "", ErrInvalidConfig
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, "", err
	}
	db, err := owner.DB()
	if err != nil {
		return nil, "", err
	}
	if err := migrate.CheckCurrent(ctx, db); err != nil {
		return nil, "", err
	}
	if cfg.Business.ExecutionVenue == native.Venue {
		if err := himalayamigrate.Check(ctx, db); err != nil {
			return nil, "", err
		}
	}
	incarnation, err := ownerid.New(ctx, cfg.InstanceId)
	if err != nil {
		return nil, "", err
	}
	return db, incarnation, nil
}
