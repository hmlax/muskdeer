// Repository constructors for service tests. Keep fields aligned with production constructors.
package data

import (
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	"github.com/tx7do/go-utils/mapper"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	modelApi "github.com/hmlax/muskdeer/operator/internal/data/store/api"
	modelMenu "github.com/hmlax/muskdeer/operator/internal/data/store/menu"
	modelPermissionGroup "github.com/hmlax/muskdeer/operator/internal/data/store/permissiongroup"
	modelPosition "github.com/hmlax/muskdeer/operator/internal/data/store/position"
)

// NewPositionRepoForTest 与生产 NewPositionRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewPositionRepoForTest(dbClient *store.DBClient) *PositionRepo {
	repo := &PositionRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Position, store.Position](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Position_Status, modelPosition.Status](
			operatorV1.Position_Status_name, operatorV1.Position_Status_value,
		),
		typeConverter: mapper.NewEnumTypeConverter[operatorV1.Position_Type, modelPosition.Type](
			operatorV1.Position_Type_name, operatorV1.Position_Type_value,
		),
	}

	repo.init()

	return repo
}

// NewApiRepoForTest 与生产 NewApiRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewApiRepoForTest(dbClient *store.DBClient) *ApiRepo {
	repo := &ApiRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Api, store.Api](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Api_Status, modelApi.Status](
			operatorV1.Api_Status_name, operatorV1.Api_Status_value,
		),
		scopeConverter: mapper.NewEnumTypeConverter[operatorV1.Api_Scope, modelApi.Scope](
			operatorV1.Api_Scope_name, operatorV1.Api_Scope_value,
		),
		businessModuleConverter: mapper.NewEnumTypeConverter[operatorV1.Module_Module, modelApi.BusinessModule](
			operatorV1.Module_Module_name, operatorV1.Module_Module_value,
		),
	}

	repo.init()

	return repo
}

// NewPermissionGroupRepoForTest 与生产 NewPermissionGroupRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewPermissionGroupRepoForTest(dbClient *store.DBClient) *PermissionGroupRepo {
	repo := &PermissionGroupRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.PermissionGroup, store.PermissionGroup](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.PermissionGroup_Status, modelPermissionGroup.Status](
			operatorV1.PermissionGroup_Status_name, operatorV1.PermissionGroup_Status_value,
		),
	}

	repo.init()

	return repo
}

// NewMenuRepoForTest 与生产 NewMenuRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewMenuRepoForTest(dbClient *store.DBClient) *MenuRepo {
	repo := &MenuRepo{
		log:             bLogger.NewHelper(bLogger.NopLogger()),
		dbClient:        dbClient,
		mapper:          mapper.NewCopierMapper[operatorV1.Menu, store.Menu](),
		statusConverter: mapper.NewEnumTypeConverter[operatorV1.Menu_Status, modelMenu.Status](operatorV1.Menu_Status_name, operatorV1.Menu_Status_value),
		typeConverter:   mapper.NewEnumTypeConverter[operatorV1.Menu_Type, modelMenu.Type](operatorV1.Menu_Type_name, operatorV1.Menu_Type_value),
		moduleConverter: mapper.NewEnumTypeConverter[operatorV1.Module_Module, modelMenu.Module](operatorV1.Module_Module_name, operatorV1.Module_Module_value),
	}

	repo.init()

	return repo
}

// NewLanguageRepoForTest 与生产 NewLanguageRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewLanguageRepoForTest(dbClient *store.DBClient) *LanguageRepo {
	repo := &LanguageRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Language, store.Language](),
	}

	repo.init()

	return repo
}

// NewDictTypeRepoForTest 与生产 NewDictTypeRepo 逐字段一致（log 换 NopLogger），并调用 init()。
func NewDictTypeRepoForTest(dbClient *store.DBClient) *DictTypeRepo {
	repo := &DictTypeRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DictType, store.DictType](),
	}

	repo.init()

	return repo
}

// testkitDictEntryI18nRepo 私有构造器：与生产 NewDictEntryI18nRepo 逐字段一致
// （log 换 NopLogger），仅供本文件 NewDictEntryRepoForTest 组装关联子 repo 使用。
func testkitDictEntryI18nRepo(dbClient *store.DBClient) *DictEntryI18nRepo {
	repo := &DictEntryI18nRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DictEntryI18N, store.DictEntryI18n](),
	}

	repo.init()

	return repo
}

// NewDictEntryRepoForTest 与生产 NewDictEntryRepo 逐字段一致（log 换 NopLogger，
// 关联 i18n 子 repo 由 testkitDictEntryI18nRepo 组装），并调用 init()。
func NewDictEntryRepoForTest(dbClient *store.DBClient) *DictEntryRepo {
	repo := &DictEntryRepo{
		log:      bLogger.NewHelper(bLogger.NopLogger()),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DictEntry, store.DictEntry](),
		i18n:     testkitDictEntryI18nRepo(dbClient),
	}

	repo.init()

	return repo
}
