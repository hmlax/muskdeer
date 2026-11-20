package data

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"github.com/tx7do/go-utils/trans"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"

	"github.com/hmlax/muskdeer/operator/internal/data/store"
	"github.com/hmlax/muskdeer/operator/internal/data/store/file"

	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"

	"github.com/tx7do/go-utils/copierutil"
	"github.com/tx7do/go-utils/mapper"
)

type FileRepo struct {
	dbClient *store.DBClient
	log      *bLogger.Helper

	mapper            *mapper.CopierMapper[operatorV1.File, store.File]
	providerConverter *mapper.EnumTypeConverter[operatorV1.OSSProvider_OSSProvider, file.Provider]

	repository *store.Repository[operatorV1.File, store.File]
}

func NewFileRepo(ctx *bootstrap.Context, dbClient *store.DBClient) *FileRepo {
	repo := &FileRepo{
		log:               ctx.NewLoggerHelper("file/repo/admin-service"),
		dbClient:          dbClient,
		mapper:            mapper.NewCopierMapper[operatorV1.File, store.File](),
		providerConverter: mapper.NewEnumTypeConverter[operatorV1.OSSProvider_OSSProvider, file.Provider](operatorV1.OSSProvider_OSSProvider_name, operatorV1.OSSProvider_OSSProvider_value),
	}

	repo.init()

	return repo
}

func (r *FileRepo) init() {
	r.repository = store.NewRepository[operatorV1.File, store.File](r.mapper)

	r.mapper.AppendConverters(copierutil.NewTimeStringConverterPair())
	r.mapper.AppendConverters(copierutil.NewTimeTimestamppbConverterPair())

	r.mapper.AppendConverters(r.providerConverter.NewConverterPair())
}

// formatSize 返回格式化后的文本，例如 "512B", "1.5KB"。
// 对字节单位返回整数；对其它单位保留最多两位小数并去掉多余的 0。
func (r *FileRepo) formatSize(size int64) string {
	if size <= 0 {
		return "0B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	s := decimal.NewFromInt(size)
	i := 0
	for s.GreaterThanOrEqual(decimal.NewFromInt(1024)) && i < len(units)-1 {
		s = s.Div(decimal.NewFromInt(1024))
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d%s", size, units[i])
	}
	str := s.Round(2).String()
	return str + units[i]
}

func (r *FileRepo) Count(ctx context.Context, whereCond []queryscope.Scope) (int, error) {
	builder := r.dbClient.Client().File.Query()
	if len(whereCond) != 0 {
		builder.Modify(whereCond...)
	}

	count, err := builder.Count(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query count failed: %s", err.Error())
		return 0, operatorV1.ErrorStorageInternalServerError("query count failed")
	}

	return count, nil
}

func (r *FileRepo) List(ctx context.Context, req *paginationV1.PagingRequest) (*operatorV1.ListFileResponse, error) {
	if req == nil {
		return nil, operatorV1.ErrorStorageBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().File.Query()

	ret, err := r.repository.ListWithPaging(ctx, builder, builder.Clone(), req)
	if err != nil {
		return nil, err
	}
	if ret == nil {
		return &operatorV1.ListFileResponse{
			Total: 0,
			Items: nil,
		}, nil
	}

	return &operatorV1.ListFileResponse{
		Total: ret.Total,
		Items: ret.Items,
	}, nil
}

func (r *FileRepo) IsExist(ctx context.Context, id uint32) (bool, error) {
	exist, err := r.dbClient.Client().File.Query().
		Where(file.IDEQ(id)).
		Exist(ctx)
	if err != nil {
		r.log.Errorf(ctx, "query exist failed: %s", err.Error())
		return false, operatorV1.ErrorStorageInternalServerError("query exist failed")
	}
	return exist, nil
}

func (r *FileRepo) Get(ctx context.Context, req *operatorV1.GetFileRequest) (*operatorV1.File, error) {
	if req == nil {
		return nil, operatorV1.ErrorStorageBadRequest("invalid parameter")
	}

	builder := r.dbClient.Client().File.Query()

	var whereCond []queryscope.Scope
	switch req.QueryBy.(type) {
	default:
	case *operatorV1.GetFileRequest_Id:
		whereCond = append(whereCond, file.IDEQ(req.GetId()))
	}

	dto, err := r.repository.Get(ctx, builder, req.GetViewMask(), whereCond...)
	if err != nil {
		return nil, err
	}

	return dto, err
}

func (r *FileRepo) Create(ctx context.Context, req *operatorV1.CreateFileRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorStorageBadRequest("invalid parameter")
	}

	if req.Data.Size != nil {
		req.Data.SizeFormat = trans.Ptr(r.formatSize(int64(req.Data.GetSize())))
	}

	builder := r.dbClient.Client().File.Create().
		SetNillableTenantID(req.Data.TenantId).
		SetNillableProvider(r.providerConverter.ToEntity(req.Data.Provider)).
		SetNillableBucketName(req.Data.BucketName).
		SetNillableFileDirectory(req.Data.FileDirectory).
		SetNillableFileGUID(req.Data.FileGuid).
		SetNillableSaveFileName(req.Data.SaveFileName).
		SetNillableFileName(req.Data.FileName).
		SetNillableExtension(req.Data.Extension).
		SetNillableSize(req.Data.Size).
		SetNillableSizeFormat(req.Data.SizeFormat).
		SetNillableLinkURL(req.Data.LinkUrl).
		SetNillableContentHash(req.Data.ContentHash).
		SetNillableCreatedBy(req.Data.CreatedBy).
		SetCreatedAt(time.Now())

	if req.Data.Id != nil {
		builder.SetID(req.GetData().GetId())
	}

	if err := builder.Exec(ctx); err != nil {
		r.log.Errorf(ctx, "insert file failed: %s", err.Error())
		return operatorV1.ErrorStorageInternalServerError("insert file failed")
	}

	return nil
}

func (r *FileRepo) Update(ctx context.Context, req *operatorV1.UpdateFileRequest) error {
	if req == nil || req.Data == nil {
		return operatorV1.ErrorStorageBadRequest("invalid parameter")
	}
	if req.GetId() == 0 {
		return operatorV1.ErrorStorageBadRequest("id is required")
	}

	if req.Data.Size != nil {
		req.Data.SizeFormat = trans.Ptr(r.formatSize(int64(req.Data.GetSize())))
	}

	// 如果不存在则创建
	if req.GetAllowMissing() {
		exist, err := r.IsExist(ctx, req.GetId())
		if err != nil {
			return err
		}
		if !exist {
			createReq := &operatorV1.CreateFileRequest{Data: req.Data}
			createReq.Data.CreatedBy = createReq.Data.UpdatedBy
			createReq.Data.UpdatedBy = nil
			return r.Create(ctx, createReq)
		}
	}

	builder := r.dbClient.Client().File.Update()
	err := r.repository.UpdateX(ctx, builder, req.Data, req.GetUpdateMask(),
		func(dto *operatorV1.File) {
			builder.
				SetNillableProvider(r.providerConverter.ToEntity(req.Data.Provider)).
				SetNillableBucketName(req.Data.BucketName).
				SetNillableFileDirectory(req.Data.FileDirectory).
				SetNillableFileGUID(req.Data.FileGuid).
				SetNillableSaveFileName(req.Data.SaveFileName).
				SetNillableFileName(req.Data.FileName).
				SetNillableExtension(req.Data.Extension).
				SetNillableSize(req.Data.Size).
				SetNillableSizeFormat(req.Data.SizeFormat).
				SetNillableLinkURL(req.Data.LinkUrl).
				SetNillableContentHash(req.Data.ContentHash).
				SetNillableCreatedBy(req.Data.UpdatedBy).
				SetUpdatedAt(time.Now())
		},
		queryscope.Equal(file.FieldID, req.GetId()),
	)

	return err
}

func (r *FileRepo) Delete(ctx context.Context, req *operatorV1.DeleteFileRequest) error {
	if req == nil {
		return operatorV1.ErrorStorageBadRequest("invalid parameter")
	}

	if err := r.dbClient.Client().File.DeleteOneID(req.GetId()).Exec(ctx); err != nil {
		if store.IsNotFound(err) {
			return operatorV1.ErrorStorageNotFound("file not found")
		}

		r.log.Errorf(ctx, "delete one data failed: %s", err.Error())

		return operatorV1.ErrorStorageInternalServerError("delete failed")
	}

	return nil
}
