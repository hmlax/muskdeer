package service

import (
	"context"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/hmlax/muskdeer/operator/internal/data"

	"github.com/tx7do/go-crud/viewer"

	"github.com/hmlax/muskdeer/deer/management/constants"
	"github.com/hmlax/muskdeer/deer/management/middleware/auth"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

type LanguageService struct {
	operatorV1.UnimplementedLanguageServiceServer
	log *bLogger.Helper

	languageRepo *data.LanguageRepo
}

func NewLanguageService(
	ctx *bootstrap.Context,
	languageRepo *data.LanguageRepo,
) *LanguageService {
	svc := &LanguageService{
		log:          ctx.NewLoggerHelper("language/service/admin-service"),
		languageRepo: languageRepo,
	}

	svc.init()

	return svc
}

func (s *LanguageService) init() {
	ctx := viewer.WithSystemContext(context.Background())
	if count, _ := s.languageRepo.Count(ctx, []queryscope.Scope{}); count == 0 {
		_ = s.createDefaultLanguage(ctx)
	}
}

func (s *LanguageService) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLanguageResponse, error) {
	resp, err := s.languageRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *LanguageService) Get(ctx context.Context, req *operatorV1.GetLanguageRequest) (*operatorV1.Language, error) {
	resp, err := s.languageRepo.Get(ctx, req)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *LanguageService) Create(ctx context.Context, req *operatorV1.CreateLanguageRequest) (*emptypb.Empty, error) {
	if req.Data == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	// 获取操作人信息
	operator, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	req.Data.CreatedBy = trans.Ptr(operator.UserId)

	if err = s.languageRepo.Create(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LanguageService) Update(ctx context.Context, req *operatorV1.UpdateLanguageRequest) (*emptypb.Empty, error) {
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

	if err = s.languageRepo.Update(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

func (s *LanguageService) BatchCreate(ctx context.Context, req *operatorV1.BatchCreateLanguagesRequest) (*emptypb.Empty, error) {
	actor, err := auth.FromContext(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range req.GetItems() {
		if item != nil {
			item.CreatedBy = trans.Ptr(actor.UserId)
		}
	}
	if err := s.languageRepo.BatchCreate(ctx, req); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *LanguageService) Delete(ctx context.Context, req *operatorV1.DeleteLanguageRequest) (*emptypb.Empty, error) {
	if err := s.languageRepo.Delete(ctx, req); err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, nil
}

// createDefaultLanguage 创建默认语言
func (s *LanguageService) createDefaultLanguage(ctx context.Context) (err error) {
	for _, user := range constants.DefaultLanguages {
		if err = s.languageRepo.Create(ctx, &operatorV1.CreateLanguageRequest{
			Data: user,
		}); err != nil {
			s.log.Errorf(ctx, "create default language err: %v", err)
			return err
		}
	}

	return err
}
