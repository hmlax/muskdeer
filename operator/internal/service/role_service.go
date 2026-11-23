package service

import (
	"context"
	"errors"
	"fmt"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/aggregator"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/tx7do/go-crud/viewer"
	"github.com/tx7do/go-utils/authorizer"
	"github.com/tx7do/go-utils/sliceutil"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type RoleService struct {
	operatorV1.UnimplementedRoleServiceServer
	log *bLogger.Helper

	authorizer *authorizer.Authorizer

	roleRepo      *data.RoleRepo
	tenantRepo    *data.TenantRepo
	userRoleRepo  *data.UserRoleRepo
	authenticator *data.Authenticator
}

const permissionGrantDeniedMessage = "cannot grant permissions not held by current user"

func (s *RoleService) validatePermissionGrant(ctx context.Context, actor *operatorV1.UserTokenPayload, permissionIDs []uint32) error {
	if actor.GetTenantId() == 0 || actor.GetIsPlatformAdmin() || len(permissionIDs) == 0 {
		return nil
	}
	owned, err := s.roleRepo.ListPermissionIDsByRoleCodes(ctx, actor.GetRoles())
	if err != nil {
		return err
	}
	allowed := make(map[uint32]struct{}, len(owned))
	for _, identifier := range owned {
		allowed[identifier] = struct{}{}
	}
	for _, identifier := range permissionIDs {
		if _, ok := allowed[identifier]; !ok {
			return operatorV1.ErrorPermissionForbidden(permissionGrantDeniedMessage)
		}
	}
	return nil
}

func NewRoleService(
	ctx *bootstrap.Context,
	authorizer *authorizer.Authorizer,
	roleRepo *data.RoleRepo,
	tenantRepo *data.TenantRepo,
	userRoleRepo *data.UserRoleRepo,
	authenticator *data.Authenticator,
) *RoleService {
	svc := &RoleService{
		log:           ctx.NewLoggerHelper("role/service/admin-service"),
		authorizer:    authorizer,
		roleRepo:      roleRepo,
		tenantRepo:    tenantRepo,
		userRoleRepo:  userRoleRepo,
		authenticator: authenticator,
	}

	svc.init()

	return svc
}

func (s *RoleService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.roleRepo.Count(ctx, nil); count == 0 {
		_ = s.createDefaultRoles(ctx)
	}
}

func (s *RoleService) extractRelationIDs(
	roles []*operatorV1.Role,
	tenantSet aggregator.ResourceMap[uint32, *operatorV1.Tenant],
) {
	for _, p := range roles {
		if p.GetTenantId() > 0 {
			tenantSet[p.GetTenantId()] = nil
		}
	}
}

func (s *RoleService) fetchRelationInfo(
	ctx context.Context,
	tenantSet aggregator.ResourceMap[uint32, *operatorV1.Tenant],
) error {
	if len(tenantSet) > 0 {
		tenantIds := make([]uint32, 0, len(tenantSet))
		for id := range tenantSet {
			tenantIds = append(tenantIds, id)
		}

		tenants, err := s.tenantRepo.ListTenantsByIds(ctx, tenantIds)
		if err != nil {
			s.log.Errorf(context.Background(), "query tenants err: %v", err)
			return err
		}

		for _, tenant := range tenants {
			tenantSet[tenant.GetId()] = tenant
		}
	}

	return nil
}

func (s *RoleService) bindRelations(
	roles []*operatorV1.Role,
	tenantSet aggregator.ResourceMap[uint32, *operatorV1.Tenant],
) {
	aggregator.Populate(
		roles,
		tenantSet,
		func(ou *operatorV1.Role) uint32 { return ou.GetTenantId() },
		func(ou *operatorV1.Role, r *operatorV1.Tenant) {
			ou.TenantName = r.Name
		},
	)
}

func (s *RoleService) enrichRelations(ctx context.Context, roles []*operatorV1.Role) error {
	var tenantSet = make(aggregator.ResourceMap[uint32, *operatorV1.Tenant])
	s.extractRelationIDs(roles, tenantSet)
	if err := s.fetchRelationInfo(ctx, tenantSet); err != nil {
		return err
	}
	s.bindRelations(roles, tenantSet)
	return nil
}

func (s *RoleService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListRoleResponse, error) {
	resp, err := s.roleRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	_ = s.enrichRelations(ctx, resp.Items)

	return resp, nil
}

func (s *RoleService) Count(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.CountRoleResponse, error) {
	count, err := s.roleRepo.Count(ctx, req)
	if err != nil {
		return nil, err
	}

	return &operatorV1.CountRoleResponse{
		Count: uint64(count),
	}, nil
}

func (s *RoleService) Get(ctx context.Context, req *operatorV1.GetRoleRequest) (*operatorV1.Role, error) {
	resp, err := s.roleRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	fakeItems := []*operatorV1.Role{resp}
	_ = s.enrichRelations(ctx, fakeItems)

	return resp, nil
}

func (s *RoleService) Create(ctx context.Context, req *operatorV1.CreateRoleRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.validatePermissionGrant(ctx, operator, req.Data.GetPermissions()); err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if operator.GetTenantId() > 0 {
		req.Data.Type = trans.Ptr(operatorV1.Role_TENANT)
		req.Data.TenantId = operator.TenantId
	}

	if err = s.roleRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleService) Update(ctx context.Context, req *operatorV1.UpdateRoleRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		s.log.Errorf(ctx, "get operator from context error: %v", err)
		return nil, err
	}
	if updatesField(req.UpdateMask, "permissions") {
		if err := s.validatePermissionGrant(ctx, operator, req.Data.GetPermissions()); err != nil {
			return nil, err
		}
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if operator.GetTenantId() > 0 && req.Data.GetType() != operatorV1.Role_TENANT {
		req.Data.Type = trans.Ptr(operatorV1.Role_TENANT)
	}

	r, err := s.roleRepo.Get(ctx, &operatorV1.GetRoleRequest{
		QueryBy: &operatorV1.GetRoleRequest_Id{
			Id: req.GetId(),
		},
	})
	if err != nil {
		return nil, err
	}

	// 非系统管理员禁止修改系统角色
	if r.GetType() == operatorV1.Role_SYSTEM && operator.GetTenantId() > 0 {
		return nil, operatorV1.ErrorAdminForbidden("no permission to update system role")
	}

	// 保护角色字段不可修改
	if r.GetIsProtected() {
		if len(req.GetUpdateMask().Paths) > 0 {
			req.GetUpdateMask().Paths = sliceutil.FilterBlacklist(req.GetUpdateMask().Paths, []string{
				"is_protected",
				"type",
				"status",
				"code",
			})
		} else {
			req.Data.IsProtected = nil
			req.Data.Type = nil
			req.Data.Status = nil
			req.Data.Code = nil
		}
	}

	refreshClaims := roleChangesTokenClaims(req)
	if updatesField(req.UpdateMask, "status") && req.Data.Status != nil && req.Data.GetStatus() != r.GetStatus() {
		refreshClaims = true
	}
	if err = s.roleRepo.Update(ctx, req); err != nil {
		s.log.Errorf(ctx, "update role error: %v", err)
		return nil, err
	}
	if refreshClaims {
		if err := s.revokeRoleSessions(ctx, req.GetId()); err != nil {
			return nil, err
		}
	}

	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleService) Delete(ctx context.Context, req *operatorV1.DeleteRoleRequest) (*emptypb.Empty, error) {
	users, err := s.userRoleRepo.ListUserIDs(ctx, req.GetId(), false)
	if err != nil {
		return nil, err
	}

	if err = s.roleRepo.Delete(ctx, req); err != nil {
		return nil, err
	}
	if err := s.revokeRoleUserSessions(ctx, req.GetId(), users); err != nil {
		return nil, err
	}

	if err = s.authorizer.ResetPolicies(ctx); err != nil {
		s.log.Errorf(ctx, "reset policies error: %v", err)
	}

	return &emptypb.Empty{}, nil
}

func (s *RoleService) revokeRoleSessions(ctx context.Context, roleID uint32) error {
	users, err := s.userRoleRepo.ListUserIDs(ctx, roleID, false)
	if err != nil {
		return err
	}
	return s.revokeRoleUserSessions(ctx, roleID, users)
}

func (s *RoleService) revokeRoleUserSessions(ctx context.Context, roleID uint32, users []uint32) error {
	var failures []error
	for _, userID := range users {
		if err := s.authenticator.RevokeUserTokenAllClientTypes(ctx, userID); err != nil {
			failures = append(failures, fmt.Errorf("revoke sessions for role %d user %d: %w", roleID, userID, err))
		}
	}
	return errors.Join(failures...)
}

// GetRoleCodesByRoleIds resolves role identifiers to their codes.
func (s *RoleService) GetRoleCodesByRoleIds(ctx context.Context, req *operatorV1.GetRoleCodesByRoleIdsRequest) (*operatorV1.GetRoleCodesByRoleIdsResponse, error) {
	ids, err := s.roleRepo.ListRoleCodesByRoleIds(ctx, req.GetRoleIds())
	if err != nil {
		return nil, err
	}

	return &operatorV1.GetRoleCodesByRoleIdsResponse{
		RoleCodes: ids,
	}, nil
}

func (s *RoleService) GetRolesByRoleCodes(ctx context.Context, req *operatorV1.GetRolesByRoleCodesRequest) (*operatorV1.ListRoleResponse, error) {
	roles, err := s.roleRepo.ListRolesByRoleCodes(ctx, req.GetRoleCodes())
	if err != nil {
		return nil, err
	}

	return &operatorV1.ListRoleResponse{
		Items: roles,
		Total: uint64(len(roles)),
	}, nil
}

// GetRolesByRoleIds returns the requested administration roles.
func (s *RoleService) GetRolesByRoleIds(ctx context.Context, req *operatorV1.GetRolesByRoleIdsRequest) (*operatorV1.ListRoleResponse, error) {
	roles, err := s.roleRepo.ListRolesByRoleIds(ctx, req.GetRoleIds())
	if err != nil {
		return nil, err
	}

	return &operatorV1.ListRoleResponse{
		Items: roles,
		Total: uint64(len(roles)),
	}, nil
}

// createDefaultRoles 创建默认角色(包括超级管理员)
func (s *RoleService) createDefaultRoles(ctx context.Context) error {
	var err error

	for _, d := range constants.DefaultRoles {
		err = s.roleRepo.Create(ctx, &operatorV1.CreateRoleRequest{
			Data: d,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
