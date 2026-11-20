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

type LoginAuditLogService struct {
	operatorV1.UnimplementedLoginAuditLogServiceServer
	log *bLogger.Helper

	repo *data.LoginAuditLogRepo
}

func NewLoginAuditLogService(ctx *bootstrap.Context, repo *data.LoginAuditLogRepo) *LoginAuditLogService {
	return &LoginAuditLogService{
		log:  ctx.NewLoggerHelper("login-audit-log/service/admin-service"),
		repo: repo,
	}
}

func (s *LoginAuditLogService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLoginAuditLogResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *LoginAuditLogService) Get(ctx context.Context, req *operatorV1.GetLoginAuditLogRequest) (*operatorV1.LoginAuditLog, error) {
	return s.repo.Get(ctx, req)
}

func (s *LoginAuditLogService) Create(ctx context.Context, req *operatorV1.CreateLoginAuditLogRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	if err := s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
