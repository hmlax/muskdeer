package data

import (
	"context"
	"strconv"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	assets "github.com/hmlax/muskdeer/deer/management/apidocs"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/authorizer"

	"github.com/hmlax/muskdeer/deer/management/constants"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

// AuthorizerProvider 权限数据提供者
type AuthorizerProvider struct {
	log *bLogger.Helper

	roleRepo *RoleRepo
	apiRepo  *ApiRepo
}

func NewAuthorizerProvider(
	ctx *bootstrap.Context,
	roleRepo *RoleRepo,
	apiRepo *ApiRepo,
) authorizer.Provider {
	return &AuthorizerProvider{
		log:      ctx.NewLoggerHelper("authorizer-data-provider/data/admin-service"),
		roleRepo: roleRepo,
		apiRepo:  apiRepo,
	}
}

// ProvideModels 提供模型数据
func (p *AuthorizerProvider) ProvideModels(engineName string) authorizer.ModelDataMap {
	switch engineName {
	case "casbin":
		return make(map[string][]byte)
	case "opa":
		return map[string][]byte{
			"rbac.rego": assets.OpaRbacRego,
		}
	}
	return nil
}

// ProvidePolicies 提供策略数据
func (p *AuthorizerProvider) ProvidePolicies(ctx context.Context) (authorizer.PermissionDataMap, error) {
	// 策略装载需要全量角色/权限数据，统一以 SystemContext 跑，忽略调用方 ctx 的 viewer
	ctx = viewer.WithSystemContext(ctx)

	roles, err := p.roleRepo.List(ctx, &paginationV1.PagingRequest{NoPaging: trans.Ptr(true)})
	if err != nil {
		p.log.Errorf(ctx, "failed to list roles: %v", err)
		return nil, err
	}

	result := make(authorizer.PermissionDataMap)
	var apiIDs []uint32
	var apis []*operatorV1.Api
	for _, role := range roles.Items {
		// p.log.Infof(context.Background(), _, "processing role: %s", role.GetCode())
		if role == nil {
			continue
		}
		if role.GetCode() == "" {
			continue
		}
		if role.GetStatus() != operatorV1.Role_ON {
			continue
		}
		if constants.IsTemplateRoleCode(role.GetCode()) {
			continue
		}

		apiIDs, err = p.roleRepo.GetRolePermissionApiIDs(ctx, role.GetId())
		if err != nil {
			p.log.Errorf(ctx, "failed to get role [%d] permission api ids: %v", role.GetId(), err)
			continue
		}

		// 角色未绑定任何 API 时直接跳过：GetApiByIDs 对空集合返回 400，
		// 逐角色刷 ERROR 日志纯属误导。
		if len(apiIDs) == 0 {
			continue
		}

		apis, err = p.apiRepo.GetApiByIDs(ctx, apiIDs)
		if err != nil {
			p.log.Errorf(ctx, "failed to list apis by ids: %v", err)
			continue
		}

		var authorizerDataArray authorizer.PermissionDataArray
		for _, api := range apis {
			if api == nil {
				continue
			}
			if api.GetPath() == "" || api.GetMethod() == "" {
				continue
			}

			var data = authorizer.PermissionData{
				Domain: strconv.FormatUint(uint64(role.GetTenantId()), 10),
				Path:   api.GetPath(),
				Method: api.GetMethod(),
			}
			authorizerDataArray = append(authorizerDataArray, data)
		}
		if len(authorizerDataArray) > 0 {
			// Role codes are tenant-local; preserve every domain instead of
			// allowing the last tenant's same-named role to replace the others.
			result[role.GetCode()] = append(result[role.GetCode()], authorizerDataArray...)
		}
	}

	return result, nil
}
