package data

import (
	"context"
	"time"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/dicttype"
)

type DictTypeRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper *mapper.CopierMapper[operatorV1.DictType, store.DictType]

	repository *store.Repository[operatorV1.DictType, store.DictType]
}

func NewDictTypeRepo(
	ctx *bootstrap.Context,
	dbClient *store.DBClient,
) *DictTypeRepo {
	repo := &DictTypeRepo{
		log:      ctx.NewLoggerHelper("dict-type/repo/admin-service"),
		dbClient: dbClient,
		mapper:   mapper.NewCopierMapper[operatorV1.DictType, store.DictType](),
	}

	repo.init()

	return repo
}

func (r *DictTypeRepo) init() {
	r.repository = store.NewRepository[operatorV1.DictType, store.DictType](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())
}

func (r *DictTypeRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().DictType.Query()
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

func (r *DictTypeRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListDictTypeResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DictType.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListDictTypeResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListDictTypeResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *DictTypeRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().DictType.Query().
		Where(dicttype.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorDictInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *DictTypeRepo) Get(ctx context.Context, req *operatorV1.GetDictTypeRequest) (*operatorV1.DictType, error) {
	if req == nil {
		return nil, operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().DictType.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetDictTypeRequest_Id:
		whereCond = append(whereCond, dicttype.IDEQ(req.GetId()))
	case *operatorV1.GetDictTypeRequest_Code:
		// type_code 仅在 (tenant_id, type_code) 维度唯一，平台上下文(tid=0)下按 type_code 查询
		// 会跨租户匹配多行导致 .Only() 报 not singular，且会泄露其他租户字典类型。
		// 仅允许在具名租户上下文(tid>0)下按 type_code 查询。
		if _, hasTenant := maybeTenantFromViewer(ctx); !hasTenant {
			return nil, operatorV1.ErrorDictBadRequest("tenant scope required to query dict type by code")
		}
		builder.Where(dicttype.TypeCodeEQ(req.GetCode()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *DictTypeRepo) Create(ctx context.Context, req *operatorV1.CreateDictTypeRequest) (err error) {
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

	builder := tx.DictType.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableTypeCode(req.Data.TypeCode).
		SetNillableTypeName(req.Data.TypeName).
		SetNillableSortOrder(req.Data.SortOrder).
		SetNillableIsEnabled(req.Data.IsEnabled).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if _, err = builder.Save(ctx); err != nil {
		r.log.Errorf(ctx, "insert dict type failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("insert dict type failed")
	}

	return nil
}

func (r *DictTypeRepo) Update(ctx context.Context, req *operatorV1.UpdateDictTypeRequest) (err error) {
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
			createReq := &operatorV1.CreateDictTypeRequest{Data: req.Data}
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

	builder := tx.DictType.UpdateOneID(req.GetId())
	_, err = r.repository.UpdateOne(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.DictType) {
			builder.
				SetNillableTypeName(req.Data.TypeName).
				SetNillableSortOrder(req.Data.SortOrder).
				SetNillableIsEnabled(req.Data.IsEnabled).
				SetNillableUpdatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(dicttype.FieldID, req.GetId()),
	)
	if err != nil {
		r.log.Errorf(ctx, "update dict type failed: %s", err.Error())
		return operatorV1.ErrorDictInternalServerError("update dict type failed")
	}

	return err
}

func (r *DictTypeRepo) Delete(ctx context.Context, id uint32) error {
	if id == 0 {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	if err := r.dbClient.Client().DictType.DeleteOneID(id).Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorDictNotFound("dict not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorDictInternalServerError("delete failed")
	}

	return nil
}

func (r *DictTypeRepo) BatchDelete(ctx context.Context, ids []uint32) error {
	if len(ids) == 0 {
		return operatorV1.ErrorDictBadRequest("invalid parameter")
	}

	if _, err := r.dbClient.Client().DictType.Delete().
		Where(dicttype.IDIn(ids...)).
		Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorDictNotFound("dict not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorDictInternalServerError("delete failed")
	}

	return nil
}
