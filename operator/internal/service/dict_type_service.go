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

type DictTypeService struct {
	operatorV1.UnimplementedDictTypeServiceServer
	log *bLogger.Helper

	dictTypeRepo *data.DictTypeRepo
}

func NewDictTypeService(
	ctx *bootstrap.Context,
	dictTypeRepo *data.DictTypeRepo,
) *DictTypeService {
	return &DictTypeService{
		log:          ctx.NewLoggerHelper("dict-type/service/admin-service"),
		dictTypeRepo: dictTypeRepo,
	}
}

func (s *DictTypeService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDictTypeResponse, error) {
	return s.dictTypeRepo.List(ctx, req)
}

func (s *DictTypeService) Get(ctx context.Context, req *operatorV1.GetDictTypeRequest) (*operatorV1.DictType, error) {
	return s.dictTypeRepo.Get(ctx, req)
}

func (s *DictTypeService) Create(ctx context.Context, req *operatorV1.CreateDictTypeRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.dictTypeRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *DictTypeService) Update(ctx context.Context, req *operatorV1.UpdateDictTypeRequest) (*emptypb.Empty, error) {
	if req == nil || req.Data == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
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

	if err = s.dictTypeRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *DictTypeService) Delete(ctx context.Context, req *operatorV1.DeleteDictTypeRequest) (*emptypb.Empty, error) {
	if err := s.dictTypeRepo.BatchDelete(ctx, req.GetIds()); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}
