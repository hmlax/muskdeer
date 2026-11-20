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

type DataAccessAuditLogService struct {
	operatorV1.UnimplementedDataAccessAuditLogServiceServer
	log *bLogger.Helper

	repo *data.DataAccessAuditLogRepo
}

func NewDataAccessAuditLogService(ctx *bootstrap.Context, repo *data.DataAccessAuditLogRepo) *DataAccessAuditLogService {
	return &DataAccessAuditLogService{
		log:  ctx.NewLoggerHelper("data-access-audit-log/service/admin-service"),
		repo: repo,
	}
}

func (s *DataAccessAuditLogService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDataAccessAuditLogResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *DataAccessAuditLogService) Get(ctx context.Context, req *operatorV1.GetDataAccessAuditLogRequest) (*operatorV1.DataAccessAuditLog, error) {
	return s.repo.Get(ctx, req)
}

func (s *DataAccessAuditLogService) Create(ctx context.Context, req *operatorV1.CreateDataAccessAuditLogRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	if err := s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
