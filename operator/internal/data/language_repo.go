package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/language"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

const (
	languageInvalidParameter = "invalid parameter"
	languageInsertFailed     = "insert data failed"
)

type LanguageRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.Language, store.Language]

	repository *store.Repository[operatorV1.Language, store.Language]
}

func NewLanguageRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *LanguageRepo {
	repo := &LanguageRepo{
		log:      ctx.NewLoggerHelper("language/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.Language, store.Language](),
	}

	repo.init()

	return repo
}

func (r *LanguageRepo) init() {
	r.repository = store.NewRepository[operatorV1.Language, store.Language](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
}

func (r *LanguageRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().Language.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorDictInternalServerError("query count failed")
	}

	return count, nil
}

func (r *LanguageRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListLanguageResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}

	builder := r.dbClient.Client().Language.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListLanguageResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListLanguageResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *LanguageRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().Language.Query().
		Where(language.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorDictInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *LanguageRepo) Get(ctx context.Context, req *operatorV1.GetLanguageRequest) (*operatorV1.Language, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}

	builder := r.dbClient.Client().Language.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetLanguageRequest_Id:
		whereCond = append(whereCond, language.IDEQ(req.GetId()))
	case *operatorV1.GetLanguageRequest_Code:
		whereCond = append(whereCond, language.LanguageCodeEQ(req.GetCode()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

// ListLanguageByIds 通过多个ID获取职位信息
func (r *LanguageRepo) ListLanguageByIds(ctx context.Context, ids []uint32) ([]*operatorV1.Language, error) {
	if len(ids) == 0 {
		return []*operatorV1.Language{}, nil
	}

	entities, err := r.dbClient.Client().Language.Query().
		Where(language.IDIn(ids...)).
		All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query language by ids failed: %s", err.Error())
		return nil, operatorV1.ErrorDictInternalServerError("query language by ids failed")
	}

	dtos := make([]*operatorV1.Language, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		dtos = append(dtos, dto)
	}

	return dtos, nil
}

func (r *LanguageRepo) Create(ctx context.Context, req *operatorV1.CreateLanguageRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}

	if err := r.languageCreate(req.Data).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert language failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError(languageInsertFailed)
	}

	return nil
}

func (r *LanguageRepo) BatchCreate(ctx context.Context, req *operatorV1.BatchCreateLanguagesRequest) error {
	if len(req.GetItems()) == 0 {
		return operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}

	builders := make([]*store.LanguageCreate, 0, len(req.Items))
	for _, item := range req.Items {
		if item == nil {
			return operatorV1.ErrorDictBadRequest(languageInvalidParameter)
		}
		builders = append(builders, r.languageCreate(item))
	}
	if err := r.dbClient.Client().Language.CreateBulk(builders...).Exec(ctx); err != nil {
		r.log.Errorf(ctx, "batch insert languages failed: %v", err)
		return operatorV1.ErrorDictInternalServerError(languageInsertFailed)
	}
	return nil
}

func (r *LanguageRepo) languageCreate(item *operatorV1.Language) *store.LanguageCreate {
	builder := r.dbClient.Client().Language.Create().
		SetLanguageName(item.GetLanguageName()).
		SetLanguageCode(item.GetLanguageCode()).
		SetNativeName(item.GetNativeName()).
		SetNillableIsEnabled(item.IsEnabled).
		SetNillableIsDefault(item.IsDefault).
		SetNillableSortOrder(item.SortOrder).
		SetNillableCreatedBy(item.CreatedBy).
		SetCreatedAt(time.Now())

	if item.Id != nil {
		builder.SetID(item.GetId())
	}
	return builder
}

func (r *LanguageRepo) Update(ctx context.Context, req *operatorV1.UpdateLanguageRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorDictBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreateLanguageRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().Language.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.Language) {
			builder.
				SetLanguageName(req.Data.GetLanguageName()).
				// SetLanguageCode(req.Data.GetLanguageCode()).
				SetNativeName(req.Data.GetNativeName()).
				SetNillableIsEnabled(req.Data.IsEnabled).
				SetNillableIsDefault(req.Data.IsDefault).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(language.FieldID, req.GetId()),
	)

	return err
}

func (r *LanguageRepo) Delete(ctx context.Context, req *operatorV1.DeleteLanguageRequest) error {
	if req == nil {
		return operatorV1.ErrorDictBadRequest(languageInvalidParameter)
	}

	builder := r.dbClient.Client().Language.Delete()

	var err error
	_, err = r.repository.Delete(ctx, builder, queryscope.Equal(language.FieldID, req.GetId()))
	if err != nil {
		r.log.Errorf(ctx, "delete language failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("delete language failed")
	}

	return nil
}
