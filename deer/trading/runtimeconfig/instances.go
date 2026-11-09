package runtimeconfig

import (
	"context"

	"gorm.io/gorm"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/migrate/schemaformat"
	"github.com/hmlax/muskdeer/deer/ownerid"
	configv1 "github.com/hmlax/muskdeer/deer/pb/common/config/v1"
	"github.com/hmlax/muskdeer/deer/trading/instanceconfig"
)

// InstancesData borrows the one Deer pool and reads current per-instance policy.
// It never reads legacy global business parameters or writes schema/configuration.
func InstancesData(ctx context.Context, cfg *configv1.ServiceConfig, owner *deer.Deer) (*gorm.DB, string, []*instanceconfig.Instance, error) {
	if ctx == nil || cfg == nil || owner == nil || cfg.VenueConnection == nil || cfg.Business != nil || cfg.Postgres == nil || cfg.Grpc == nil || cfg.GrpcSecurity.GetMtls() == nil || cfg.Nacos.GetRegistration() == nil {
		return nil, "", nil, ErrInvalidConfig
	}
	db, err := owner.DB()
	if err != nil {
		return nil, "", nil, err
	}
	if err := schemaformat.Check(ctx, db); err != nil {
		return nil, "", nil, err
	}
	instances, err := instanceconfig.LoadConfigured(ctx, db)
	if err != nil {
		return nil, "", nil, err
	}
	for _, instance := range instances {
		if instance.Parameters.Execution.OperationTimeoutMs > cfg.ShutdownTimeout.AsDuration().Milliseconds() {
			return nil, "", nil, ErrInvalidConfig
		}
	}
	incarnation, err := ownerid.New(ctx, cfg.InstanceId)
	return db, incarnation, instances, err
}
