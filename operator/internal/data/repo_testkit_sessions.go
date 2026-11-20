package data

import (
	"github.com/tx7do/go-utils/mapper"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/userrole"
)

// NewUserRoleRepoForTest uses the production converters with a quiet test logger.
func NewUserRoleRepoForTest(client *store.DBClient) *UserRoleRepo {
	return &UserRoleRepo{
		dbClient: client,
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.UserRole_Status, userrole.Status](
			operatorV1.UserRole_Status_name,
			operatorV1.UserRole_Status_value,
		),
	}
}
