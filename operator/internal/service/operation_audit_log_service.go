package service

import (
	"context"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data"
)

type OperationAuditLogService struct {
	operatorV1.UnimplementedOperationAuditLogServiceServer
	log *bLogger.Helper

	repo *data.OperationAuditLogRepo
}

func NewOperationAuditLogService(ctx *bootstrap.Context, repo *data.OperationAuditLogRepo) *OperationAuditLogService {
	return &OperationAuditLogService{
		log:  ctx.NewLoggerHelper("operation-audit-log/service/admin-service"),
		repo: repo,
	}
}

func (s *OperationAuditLogService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListOperationAuditLogResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *OperationAuditLogService) Get(ctx context.Context, req *operatorV1.GetOperationAuditLogRequest) (*operatorV1.OperationAuditLog, error) {
	return s.repo.Get(ctx, req)
}

func (s *OperationAuditLogService) Create(ctx context.Context, req *operatorV1.CreateOperationAuditLogRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	if err := s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
