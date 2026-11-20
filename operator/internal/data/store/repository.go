package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	gormFilter "github.com/tx7do/go-crud/gorm/filter"
	gormPaging "github.com/tx7do/go-crud/gorm/pagination"
	paginationFilter "github.com/tx7do/go-crud/pagination/filter"
	"github.com/tx7do/go-crud/pagination/paginator"
	paginationSorting "github.com/tx7do/go-crud/pagination/sorting"
	"github.com/tx7do/go-utils/fieldmaskutil"
	"github.com/tx7do/go-utils/mapper"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"gorm.io/gorm"
)

// Repository maps typed models and DTOs; all SQL is executed by GORM.
// Model queries and writes enforce tenancy before these helpers are applied.
type Repository[DTO, Model any] struct {
	mapper *mapper.CopierMapper[DTO, Model]
}

func NewRepository[DTO, Model any](mapper *mapper.CopierMapper[DTO, Model]) *Repository[DTO, Model] {
	return &Repository[DTO, Model]{mapper: mapper}
}

type PagingResult[DTO any] struct {
	Items []*DTO
	Total uint64
}

func (r *Repository[DTO, Model]) ConvertFilterByPagingRequest(request *paginationV1.PagingRequest) (*paginationV1.FilterExpr, error) {
	return paginationFilter.ConvertFilterByPagingRequest(request)
}
func (r *Repository[DTO, Model]) BuildListSelectorWithPaging(builder *Query[Model], request *paginationV1.PagingRequest) (where, query []queryscope.Scope, err error) {
	defer func() {
		if err != nil && builder != nil {
			// Some Count callers only need scopes; invalid filtering must still
			// fail closed even when those callers do not inspect this error.
			captured := err
			builder.Where(func(db *gorm.DB) *gorm.DB { _ = db.AddError(captured); return db })
		}
	}()
	if builder == nil {
		return nil, nil, errors.New("pagination request and query are required")
	}
	if request == nil {
		return nil, nil, nil
	}
	expression, err := r.ConvertFilterByPagingRequest(request)
	if err != nil {
		return nil, nil, err
	}
	where, err = gormFilter.NewStructuredFilter().BuildSelectors(expression)
	if err != nil {
		return nil, nil, err
	}
	query = append(query, where...)
	if mask := request.GetFieldMask(); mask != nil && len(mask.GetPaths()) > 0 {
		fieldmaskutil.NormalizeFieldMaskPaths(mask)
		meta, parseErr := modelSchema[Model]()
		if parseErr != nil {
			return nil, nil, parseErr
		}
		columns := make([]string, 0, len(mask.Paths))
		for _, path := range mask.Paths {
			field := meta.LookUpField(path)
			if field == nil {
				return nil, nil, fmt.Errorf("unknown selected field: %s", path)
			}
			columns = append(columns, field.DBName)
		}
		query = append(query, func(db *gorm.DB) *gorm.DB { return db.Select(columns) })
	}
	orders := request.GetSorting()
	if len(orders) == 0 && len(request.GetOrderBy()) > 0 {
		orders, err = paginationSorting.NewOrderByStringConverter().Convert(request.GetOrderBy())
		if err != nil {
			return nil, nil, err
		}
	}
	for _, order := range orders {
		if order == nil {
			continue
		}
		meta, parseErr := modelSchema[Model]()
		if parseErr != nil {
			return nil, nil, parseErr
		}
		normalized := fieldmaskutil.NormalizePaths([]string{order.GetField()})[0]
		field := meta.LookUpField(normalized)
		if field == nil {
			return nil, nil, fmt.Errorf("unknown sorting field: %s", order.GetField())
		}
		query = append(query, queryscope.OrderBy(field.DBName, order.GetDirection() == paginationV1.Sorting_DESC))
	}
	if request.GetNoPaging() {
		if paginator.NoPagingMaxLimit > 0 {
			query = append(query, func(db *gorm.DB) *gorm.DB { return db.Limit(paginator.NoPagingMaxLimit) })
		}
	} else {
		switch {
		case request.Page != nil && request.PageSize != nil:
			query = append(query, gormPaging.NewPagePaginator().BuildDB(int(request.GetPage()), int(request.GetPageSize())))
		case request.Offset != nil && request.Limit != nil:
			query = append(query, gormPaging.NewOffsetPaginator().BuildDB(int(request.GetOffset()), int(request.GetLimit())))
		case request.Token != nil && request.Offset != nil:
			query = append(query, gormPaging.NewTokenPaginator().BuildDB(request.GetToken(), int(request.GetOffset())))
		}
	}
	builder.Where(query...)
	return where, query, nil
}
func (r *Repository[DTO, Model]) ListWithPaging(ctx context.Context, builder, countBuilder *Query[Model], request *paginationV1.PagingRequest) (*PagingResult[DTO], error) {
	where, _, err := r.BuildListSelectorWithPaging(builder, request)
	if err != nil {
		return nil, err
	}
	records, err := builder.All(ctx)
	if err != nil {
		return nil, err
	}
	result := &PagingResult[DTO]{Items: make([]*DTO, 0, len(records))}
	for _, record := range records {
		result.Items = append(result.Items, r.mapper.ToDTO(record))
	}
	if countBuilder != nil {
		count, err := countBuilder.Where(where...).Count(ctx)
		if err != nil {
			return nil, err
		}
		result.Total = uint64(count)
	}
	return result, nil
}
func (r *Repository[DTO, Model]) Get(ctx context.Context, builder *Query[Model], mask *fieldmaskpb.FieldMask, predicates ...queryscope.Scope) (*DTO, error) {
	builder.Where(predicates...)
	fieldmaskutil.NormalizeFieldMaskPaths(mask)
	if mask != nil && len(mask.Paths) > 0 {
		meta, err := modelSchema[Model]()
		if err != nil {
			return nil, err
		}
		for _, path := range mask.Paths {
			if meta.LookUpField(path) == nil {
				return nil, fmt.Errorf("unknown selected field: %s", path)
			}
		}
		builder.Select(mask.Paths...)
	}
	record, err := builder.Only(ctx)
	if err != nil {
		return nil, err
	}
	return r.mapper.ToDTO(record), nil
}

type updateExecutor interface {
	ApplyWhere(...queryscope.Scope)
	ApplyNull(...string)
	Exec(context.Context) error
}
type updateOneExecutor[Model any] interface {
	updateExecutor
	Save(context.Context) (*Model, error)
}

func (w *write[T]) ApplyWhere(scopes ...queryscope.Scope) {
	w.predicates = append(w.predicates, scopes...)
}
func (w *write[T]) ApplyNull(fields ...string) { SetNull(fields...)(w.values) }

func prepareUpdate[DTO any](builder updateExecutor, data *DTO, mask *fieldmaskpb.FieldMask, apply func(*DTO), scopes []queryscope.Scope) error {
	if data == nil {
		return errors.New("update data is required")
	}
	message, ok := any(data).(proto.Message)
	if !ok {
		return errors.New("update DTO must implement proto.Message")
	}
	fieldmaskutil.NormalizeFieldMaskPaths(mask)
	if err := fieldmaskutil.FilterByFieldMask(&message, mask); err != nil {
		return err
	}
	builder.ApplyWhere(scopes...)
	if apply != nil {
		apply(data)
	}
	if mask != nil {
		nulls := fieldmaskutil.NilValuePaths(message, mask.Paths)
		// JSON subfields are patched by the resource repository rather than
		// mistaken for physical database columns.
		fields := make([]string, 0, len(nulls))
		for _, field := range nulls {
			if !strings.Contains(field, ".") {
				fields = append(fields, field)
			}
		}
		builder.ApplyNull(fields...)
	}
	return nil
}
func (r *Repository[DTO, Model]) UpdateX(ctx context.Context, builder updateExecutor, data *DTO, mask *fieldmaskpb.FieldMask, apply func(*DTO), scopes ...queryscope.Scope) error {
	if err := prepareUpdate(builder, data, mask, apply, scopes); err != nil {
		return err
	}
	return builder.Exec(ctx)
}
func (r *Repository[DTO, Model]) UpdateOne(ctx context.Context, builder updateOneExecutor[Model], data *DTO, mask *fieldmaskpb.FieldMask, apply func(*DTO), scopes ...queryscope.Scope) (*DTO, error) {
	if err := prepareUpdate(builder, data, mask, apply, scopes); err != nil {
		return nil, err
	}
	record, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	return r.mapper.ToDTO(record), nil
}
func (r *Repository[DTO, Model]) Delete(ctx context.Context, builder *Delete[Model], scopes ...queryscope.Scope) (int, error) {
	return builder.Where(scopes...).Exec(ctx)
}
