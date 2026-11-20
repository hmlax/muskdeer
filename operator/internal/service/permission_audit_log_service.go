package service

import (
	"context"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
)

type PermissionAuditLogService struct {
	operatorV1.UnimplementedPermissionAuditLogServiceServer
	log *bLogger.Helper

	permissionAuditLogRepo *data.PermissionAuditLogRepo
}

func NewPermissionAuditLogService(
	ctx *bootstrap.Context,
	permissionAuditLogRepo *data.PermissionAuditLogRepo,
) *PermissionAuditLogService {
	return &PermissionAuditLogService{
		log:                    ctx.NewLoggerHelper("permission-audit-log/service/admin-service"),
		permissionAuditLogRepo: permissionAuditLogRepo,
	}
}

func (s *PermissionAuditLogService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPermissionAuditLogResponse, error) {
	resp, err := s.permissionAuditLogRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *PermissionAuditLogService) Get(ctx context.Context, req *operatorV1.GetPermissionAuditLogRequest) (*operatorV1.PermissionAuditLog, error) {
	resp, err := s.permissionAuditLogRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *PermissionAuditLogService) Create(ctx context.Context, req *operatorV1.CreatePermissionAuditLogRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	if err := s.permissionAuditLogRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
