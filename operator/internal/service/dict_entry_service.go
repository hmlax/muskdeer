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

type DictEntryService struct {
	operatorV1.UnimplementedDictEntryServiceServer
	log *bLogger.Helper

	dictEntryRepo *data.DictEntryRepo
}

func NewDictEntryService(
	ctx *bootstrap.Context,
	dictEntryRepo *data.DictEntryRepo,
) *DictEntryService {
	return &DictEntryService{
		log:           ctx.NewLoggerHelper("dict-entry/service/admin-service"),
		dictEntryRepo: dictEntryRepo,
	}
}

func (s *DictEntryService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDictEntryResponse, error) {
	return s.dictEntryRepo.List(ctx, req)
}

func (s *DictEntryService) Create(ctx context.Context, req *operatorV1.CreateDictEntryRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.dictEntryRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *DictEntryService) Update(ctx context.Context, req *operatorV1.UpdateDictEntryRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
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

	if err = s.dictEntryRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *DictEntryService) Delete(ctx context.Context, req *operatorV1.DeleteDictEntryRequest) (*emptypb.Empty, error) {
	if err := s.dictEntryRepo.BatchDelete(ctx, req.GetIds()); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *DictEntryService) ListByTypeCode(ctx context.Context, req *operatorV1.ListDictEntryByTypeCodeRequest) (*operatorV1.ListDictEntryByTypeCodeResponse, error) {
	if req.GetTypeCode() == "" {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	return s.dictEntryRepo.ListByTypeCode(ctx, req)
}
