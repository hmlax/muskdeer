package service

import (
	"context"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type LoginPolicyService struct {
	operatorV1.UnimplementedLoginPolicyServiceServer
	log *bLogger.Helper

	repo *data.LoginPolicyRepo
}

func NewLoginPolicyService(ctx *bootstrap.Context, repo *data.LoginPolicyRepo) *LoginPolicyService {
	return &LoginPolicyService{
		log:  ctx.NewLoggerHelper("login-policy/service/admin-service"),
		repo: repo,
	}
}

func (s *LoginPolicyService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLoginPolicyResponse, error) {
	return s.repo.List(ctx, req)
}

func (s *LoginPolicyService) Get(ctx context.Context, req *operatorV1.GetLoginPolicyRequest) (*operatorV1.LoginPolicy, error) {
	return s.repo.Get(ctx, req)
}

func (s *LoginPolicyService) Create(ctx context.Context, req *operatorV1.CreateLoginPolicyRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid request")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	// 值格式校验：配错格式会让匹配器静默不命中（白名单 = 全员被锁）
	if verr := data.ValidateLoginPolicyValue(req.Data.GetMethod().String(), req.Data.GetValue()); verr != nil {
		return nil, operatorV1.ErrorAdminBadRequest("%s", verr.Error())
	}

	if err = s.repo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LoginPolicyService) Update(ctx context.Context, req *operatorV1.UpdateLoginPolicyRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid request")
	}

	// 值格式校验（理由同 Create）
	if verr := data.ValidateLoginPolicyValue(req.Data.GetMethod().String(), req.Data.GetValue()); verr != nil {
		return nil, operatorV1.ErrorAdminBadRequest("%s", verr.Error())
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.Id = trans.Ptr(req.GetId())

	req.Data.UpdatedBy = trans.Ptr(operator.UserId)
	if req.UpdateMask != nil {
		req.UpdateMask.Paths = append(req.UpdateMask.Paths, "updated_by")
	}

	if err = s.repo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LoginPolicyService) Delete(ctx context.Context, req *operatorV1.DeleteLoginPolicyRequest) (*emptypb.Empty, error) {
	if req == nil {
		return nil, operatorV1.ErrorAdminBadRequest("invalid request")
	}

	if err := s.repo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
