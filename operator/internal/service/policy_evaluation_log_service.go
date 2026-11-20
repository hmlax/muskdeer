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

type PolicyEvaluationLogService struct {
	operatorV1.UnimplementedPolicyEvaluationLogServiceServer
	log *bLogger.Helper

	policyEvaluationLogRepo *data.PolicyEvaluationLogRepo
}

func NewPolicyEvaluationLogService(
	ctx *bootstrap.Context,
	policyEvaluationLogRepo *data.PolicyEvaluationLogRepo,
) *PolicyEvaluationLogService {
	return &PolicyEvaluationLogService{
		log:                     ctx.NewLoggerHelper("policy-evaluation-log/service/admin-service"),
		policyEvaluationLogRepo: policyEvaluationLogRepo,
	}
}

func (s *PolicyEvaluationLogService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListPolicyEvaluationLogResponse, error) {
	resp, err := s.policyEvaluationLogRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *PolicyEvaluationLogService) Get(ctx context.Context, req *operatorV1.GetPolicyEvaluationLogRequest) (*operatorV1.PolicyEvaluationLog, error) {
	resp, err := s.policyEvaluationLogRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *PolicyEvaluationLogService) Create(ctx context.Context, req *operatorV1.CreatePolicyEvaluationLogRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid parameter")
	}

	if err := s.policyEvaluationLogRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
