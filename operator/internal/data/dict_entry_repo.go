package data

import (
	"context"

	"strings"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/dictentry"
	"github.com/hmlax/muskdeer/operator/internal/data/store/dicttype"
)

type DictEntryRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.DictEntry, store.DictEntry]

	repository *store.Repository[operatorV1.DictEntry, store.DictEntry]

	i18n *DictEntryI18nRepo
}

func NewDictEntryRepo(
	ctx *bootstrap.Context,
	dbClient *store.DBClient,
	i18n *DictEntryI18nRepo,
) *DictEntryRepo {
	repo := &DictEntryRepo{
		log:      ctx.NewLoggerHelper("dict-entry/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DictEntry, store.DictEntry](),
		i18n:     i18n,
	}

	repo.init()

	return repo
}

func (r *DictEntryRepo) init() {
	r.repository = store.NewRepository[operatorV1.DictEntry, store.DictEntry](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
}

func (r *DictEntryRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().DictEntry.Query()
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

func (r *DictEntryRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDictEntryResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DictEntry.Query().WithDictType()

	whereSelectors, _, err := r.repository.BuildListSelectorWithPaging(builder, req)
	if err != nil {
		r.log.Errorf(ctx, "parse list param error [%s]", err.Error())
		return nil, operatorV1.ErrorPermissionBadRequest("invalid query parameter")
	}

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query dict entry list failed: %s", err.Error())
		return nil, operatorV1.ErrorPermissionInternalServerError("query dict entry list failed")
	}

	dtos := make([]*operatorV1.DictEntry, 0, len(entities))
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		if entity.DictType != nil {
			dto.TypeId = &entity.DictType.ID
			r.log.Debugf(ctx, "dict entry entity ID: %v", entity.DictType.ID)
		}
		dtos = append(dtos, dto)
	}

	count, err := r.Count(ctx, whereSelectors)
	if err != nil {
		return nil, err
	}

	var i18ns map[string]*operatorV1.DictEntryI18N
	for _, item := range dtos {
		i18ns, err = r.i18n.ListByEntryID(ctx, item.GetId())
		if err != nil {
			return nil, err
		}
		item.I18N = i18ns

		r.log.Debugf(ctx, "dict entry: %v", item)
	}

	return &operatorV1.ListDictEntryResponse{
		Total: uint64(count),
		Items: dtos,
	}, nil
}

func (r *DictEntryRepo) Get(ctx context.Context, req *operatorV1.GetDictEntryRequest) (*operatorV1.DictEntry, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DictEntry.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetDictEntryRequest_Id:
		whereCond = append(whereCond, dictentry.IDEQ(req.GetId()))
	case *operatorV1.GetDictEntryRequest_Value:
		builder.Where(dictentry.EntryValueEQ(req.GetValue()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	i18ns, err := r.i18n.ListByEntryID(ctx, dto.GetId())
	if err != nil {
		return nil, err
	}
	dto.I18N = i18ns

	return dto, err
}

func (r *DictEntryRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().DictEntry.Query().
		Where(dictentry.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorDictInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *DictEntryRepo) Create(ctx context.Context, req *operatorV1.CreateDictEntryRequest) (err error) {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
			err = operatorV1.ErrorDictInternalServerError("transaction commit failed")
		}
	}()

	builder := tx.DictEntry.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetEntryValue(req.Data.GetEntryValue()).
		SetNillableNumericValue(req.Data.NumericValue).
		SetNillableIsEnabled(req.Data.IsEnabled).
		SetNillableSortOrder(req.Data.SortOrder).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	// 此前条件写反（TypeId == nil 时才 Set），正常携带 TypeId 反而不落库。
	if req.Data.TypeId != nil {
		builder.SetDictTypeID(req.Data.GetTypeId())
	}

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	var entity *store.DictEntry
	if entity, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert dict entry failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("insert dict entry failed")
	}

	if len(req.Data.I18N) > 0 {
		if err = r.i18n.ReplaceByEntryID(
			ctx,
			tx,
			req.Data.GetTenantId(), req.Data.GetCreatedBy(),
			entity.ID,
			req.Data.I18N,
		); err != nil {
			return err
		}
	}

	return nil
}

func (r *DictEntryRepo) Update(ctx context.Context, req *operatorV1.UpdateDictEntryRequest) (err error) {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorDictBadRequest("id is required")
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		var exist bool
		exist, err = r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreateDictEntryRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	var tx *store.Tx
	tx, err = r.dbClient.Client().Tx(ctx)
	if err != nil {
		r.log.Errorf(ctx, "start transaction failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("start transaction failed")
	}
	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				r.log.Errorf(ctx, "transaction rollback failed: %s", rollbackErr.Error())
			}
			return
		}
		if commitErr := tx.Commit(); commitErr != nil {
			r.log.Errorf(ctx, "transaction commit failed: %s", commitErr.Error())
			err = operatorV1.ErrorDictInternalServerError("transaction commit failed")
		}
	}()

	var hasI18n bool
	var i18n map[string]*operatorV1.DictEntryI18N
	for n, p := range req.GetUpdateMask().GetPaths() {
		if strings.ToLower(p) == "i18n" {
			hasI18n = true
			mask := req.GetUpdateMask()
			mask.Paths = append(mask.Paths[:n], mask.Paths[n+1:]...)
			i18n = req.Data.I18N
			break
		}
	}

	builder := tx.DictEntry.UpdateOneID(req.GetId())
	dto, err := r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.DictEntry) {
			builder.
				SetNillableEntryValue(req.Data.EntryValue).
				SetNillableNumericValue(req.Data.NumericValue).
				SetNillableIsEnabled(req.Data.IsEnabled).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(dictentry.FieldID, req.GetId()),
	)
	if err != nil {
		r.log.Errorf(ctx, "update dict entry failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("update dict entry failed")
	}

	if hasI18n && len(i18n) > 0 {
		if err = r.i18n.ReplaceByEntryID(
			ctx,
			tx,
			req.Data.GetTenantId(),
			req.Data.GetUpdatedBy(),
			dto.GetId(),
			i18n,
		); err != nil {
			return err
		}
	}

	return err
}

func (r *DictEntryRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	if err := r.dbClient.Client().DictEntry.DeleteOneID(id).Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorDictNotFound("dict not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorDictInternalServerError("delete failed")
	}

	return nil
}

func (r *DictEntryRepo) BatchDelete(ctx context.Context, ids []uint32) error {
	if len(ids) == 0 {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	if _, err := r.dbClient.Client().DictEntry.Delete().
		Where(dictentry.IDIn(ids...)).
		Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorDictNotFound("dict not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorDictInternalServerError("delete failed")
	}

	return nil
}

func (r *DictEntryRepo) ListByTypeCode(ctx context.Context, req *operatorV1.ListDictEntryByTypeCodeRequest) (*operatorV1.ListDictEntryByTypeCodeResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DictEntry.Query().
		WithDictType().
		Where(
			dictentry.HasDictTypeWith(
				dicttype.TypeCodeEQ(req.GetTypeCode()),
			),
			dictentry.IsEnabledEQ(true),
		).
		Order(store.Asc(dictentry.FieldSortOrder))

	entities, err := builder.All(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query dict entry by type code failed: %s", err.Error())
		return nil, operatorV1.ErrorDictInternalServerError("query dict entry by type code failed")
	}

	// 与通用 List 一致：回填 TypeId（该路径全部行都经 HasDictTypeWith 过滤、
	// 必有父类型边；守卫保持与通用路径同形）。
	var dtos []*operatorV1.DictEntry
	for _, entity := range entities {
		dto := r.mapper.ToDTO(entity)
		if entity.DictType != nil {
			dto.TypeId = &entity.DictType.ID
		}
		dtos = append(dtos, dto)
	}

	if req.GetLocal() != "" {
		var i18n *operatorV1.DictEntryI18N
		for _, item := range dtos {
			i18n, err = r.i18n.GetByEntryIDAndLangCode(ctx, item.GetId(), req.GetLocal())
			if err != nil {
				return nil, err
			}
			item.I18N = map[string]*operatorV1.DictEntryI18N{
				req.GetLocal(): i18n,
			}
		}
	} else {
		var i18ns map[string]*operatorV1.DictEntryI18N
		for _, item := range dtos {
			i18ns, err = r.i18n.ListByEntryID(ctx, item.GetId())
			if err != nil {
				return nil, err
			}
			item.I18N = i18ns
		}
	}

	return &operatorV1.ListDictEntryByTypeCodeResponse{
		Items: dtos,
	}, nil
}
