// Package store executes generated typed repository operations with GORM.
// Model fields and their tags are generated exclusively from operator model proto.
package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hmlax/muskdeer/operator/internal/data/store/privacy"
	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	kratoserrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/tx7do/go-crud/viewer"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

var (
	modelSchemas     sync.Map
	ErrMissingViewer = errors.New("security: missing ViewerContext in context")
	ErrCrossTenant   = errors.New("security: tenant_id cannot be changed")
	ErrDenied        = errors.New("security: data access is denied")
)

func modelSchema[T any]() (*schema.Schema, error) {
	return schema.Parse(new(T), &modelSchemas, schema.NamingStrategy{SingularTable: true})
}

func IsNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

func recordNotFound() error {
	return kratoserrors.NotFound("NOT_FOUND", "record not found").WithCause(gorm.ErrRecordNotFound)
}
func IsConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, gorm.ErrForeignKeyViolated) {
		return true
	}
	var sqliteError interface{ Code() int }
	if errors.As(err, &sqliteError) && sqliteError.Code()&255 == 19 {
		return true
	}
	var postgresError interface{ SQLState() string }
	return errors.As(err, &postgresError) && strings.HasPrefix(postgresError.SQLState(), "23")
}

func (c *Client) Close() error {
	db, err := c.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
func (c *Client) GormDB() *gorm.DB  { return c.db }
func (c *Client) Use(hooks ...Hook) { c.hooks = append(c.hooks, hooks...) }

type Tx struct{ *Client }

func (c *Client) Tx(ctx context.Context) (*Tx, error) {
	db := c.db.WithContext(ctx).Begin()
	if db.Error != nil {
		return nil, db.Error
	}
	client := NewClient(db)
	client.hooks = c.hooks
	return &Tx{Client: client}, nil
}
func (t *Tx) Commit() error   { return t.db.Commit().Error }
func (t *Tx) Rollback() error { return t.db.Rollback().Error }

type Schema struct{ client *Client }

// Create is an explicit schema-management entry point used by isolated fixtures.
// Runtime constructors never call it.
func (s *Schema) Create(ctx context.Context, _ ...any) error {
	return s.client.db.WithContext(ctx).Migrator().CreateTable(AllModels()...)
}

type EntityClient[T any] struct {
	client    *Client
	entity    string
	dataScope bool
}

func (c *EntityClient[T]) Query() *Query[T] { return &Query[T]{entity: c} }
func (c *EntityClient[T]) Get(ctx context.Context, id uint32) (*T, error) {
	return c.Query().Where(idEquals(id)).Only(ctx)
}
func (c *EntityClient[T]) GetX(ctx context.Context, id uint32) *T {
	v, e := c.Get(ctx, id)
	if e != nil {
		panic(e)
	}
	return v
}
func (c *EntityClient[T]) Create() *Create[T] {
	return &Create[T]{write: write[T]{
		entity: c,
		values: make(map[string]any),
	}}
}
func (c *EntityClient[T]) CreateBulk(builders ...*Create[T]) *CreateBulk[T] {
	return &CreateBulk[T]{
		entity:   c,
		builders: builders,
	}
}
func (c *EntityClient[T]) Update() *Update[T] {
	return &Update[T]{write: write[T]{
		entity: c,
		values: make(map[string]any),
	}}
}
func (c *EntityClient[T]) UpdateOneID(id uint32) *UpdateOne[T] {
	u := &UpdateOne[T]{write: write[T]{
		entity:     c,
		values:     make(map[string]any),
		predicates: []queryscope.Scope{idEquals(id)},
	}}
	u.id = id
	return u
}
func (c *EntityClient[T]) UpdateOne(record *T) *UpdateOne[T] { return c.UpdateOneID(recordID(record)) }
func (c *EntityClient[T]) Delete() *Delete[T]                { return &Delete[T]{entity: c} }
func (c *EntityClient[T]) DeleteOneID(id uint32) *DeleteOne[T] {
	return &DeleteOne[T]{Delete: *c.Delete().Where(idEquals(id))}
}
func (c *EntityClient[T]) DeleteOne(record *T) *DeleteOne[T] { return c.DeleteOneID(recordID(record)) }

func idEquals(id uint32) queryscope.Scope { return queryscope.Equal("id", id) }
func recordID[T any](record *T) uint32 {
	v := reflect.ValueOf(record).Elem().FieldByName("ID")
	if v.Kind() == reflect.Uint32 {
		return uint32(v.Uint())
	}
	return 0
}

func (c *EntityClient[T]) base(ctx context.Context, predicates []queryscope.Scope, query bool) *gorm.DB {
	db := c.client.db.WithContext(ctx).Model(new(T))
	if privacy.IsDenied(ctx) {
		_ = db.AddError(ErrDenied)
		return db
	}
	meta, err := modelSchema[T]()
	if err != nil {
		_ = db.AddError(err)
		return db
	}
	for _, predicate := range predicates {
		db = predicate(db)
	}
	if meta.LookUpField("tenant_id") == nil {
		return db
	}
	vc, ok := viewer.FromContext(ctx)
	if !ok {
		_ = db.AddError(ErrMissingViewer)
		return db
	}
	if vc.IsSystemContext() || vc.IsPlatformContext() {
		return db
	}
	db = db.Where(clause.Eq{
		Column: clause.Column{Name: "tenant_id"},
		Value:  vc.TenantID(),
	})
	if query && c.dataScope {
		db = applyDataScope(db, vc)
	}
	return db
}

func applyDataScope(db *gorm.DB, vc viewer.Context) *gorm.DB {
	var expressions []clause.Expression
	for _, scope := range vc.DataScope() {
		switch scope.ScopeType {
		case viewer.ScopeTypeAll:
			return db
		case viewer.ScopeTypeNone:
			_ = db.AddError(errors.New("security: data access is explicitly denied by policy"))
			return db
		case viewer.ScopeTypeSelf:
			expressions = append(expressions, clause.Eq{
				Column: clause.Column{Name: "created_by"},
				Value:  vc.UserID(),
			})
		case viewer.ScopeTypeUnit, viewer.ScopeTypeUser:
			column := "org_unit_id"
			if scope.ScopeType == viewer.ScopeTypeUser {
				column = "created_by"
			}
			values := make([]any, len(scope.TargetIDs))
			for i, id := range scope.TargetIDs {
				values[i] = id
			}
			if len(values) > 0 {
				expressions = append(expressions, clause.IN{
					Column: clause.Column{Name: column},
					Values: values,
				})
			}
		}
	}
	if len(expressions) == 0 {
		_ = db.AddError(errors.New("security: no data scope defined for current user"))
		return db
	}
	return db.Where(clause.Or(expressions...))
}

type Query[T any] struct {
	entity     *EntityClient[T]
	predicates []queryscope.Scope
	fields     []string
	limit      *int
	offset     int
	preloads   map[string]func(*gorm.DB) *gorm.DB
	locked     bool
	groups     []string
	aggregates []AggregateFunc
}

func (q *Query[T]) Clone() *Query[T] {
	c := *q
	c.predicates = append([]queryscope.Scope{}, q.predicates...)
	c.fields = append([]string{}, q.fields...)
	return &c
}
func (q *Query[T]) Where(predicates ...queryscope.Scope) *Query[T] {
	q.predicates = append(q.predicates, predicates...)
	return q
}
func (q *Query[T]) Limit(n int) *Query[T]                      { q.limit = &n; return q }
func (q *Query[T]) Offset(n int) *Query[T]                     { q.offset = n; return q }
func (q *Query[T]) Order(orders ...queryscope.Scope) *Query[T] { return q.Where(orders...) }
func (q *Query[T]) Modify(modifiers ...queryscope.Scope) *Select[T] {
	q.Where(modifiers...)
	return &Select[T]{Query: q}
}
func (q *Query[T]) Select(fields ...string) *Select[T] {
	q.fields = append([]string{}, fields...)
	return &Select[T]{Query: q}
}
func (q *Query[T]) GroupBy(fields ...string) *Query[T] {
	q.groups = append(q.groups, fields...)
	return q
}
func (q *Query[T]) Aggregate(aggregates ...AggregateFunc) *Query[T] {
	q.aggregates = append(q.aggregates, aggregates...)
	return q
}
func (q *Query[T]) Unique(_ bool) *Query[T]      { return q }
func (q *Query[T]) ForUpdate(_ ...any) *Query[T] { q.locked = true; return q }

func (q *Query[T]) db(ctx context.Context) *gorm.DB {
	db := q.entity.base(ctx, q.predicates, true)
	meta, metaErr := modelSchema[T]()
	if metaErr != nil {
		_ = db.AddError(metaErr)
		return db
	}
	if q.locked {
		db = db.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if q.limit != nil {
		db = db.Limit(*q.limit)
	}
	if q.offset > 0 {
		db = db.Offset(q.offset)
	}
	if len(q.fields) > 0 {
		for _, field := range q.fields {
			if meta.LookUpField(field) == nil {
				_ = db.AddError(fmt.Errorf("unknown selected model field: %s", field))
				return db
			}
		}
		db = db.Select(q.fields)
	}
	if len(q.groups) > 0 {
		parts := append([]string{}, q.groups...)
		for _, aggregate := range q.aggregates {
			parts = append(parts, aggregate(db))
		}
		db = db.Select(strings.Join(parts, ","))
		for _, field := range q.groups {
			db = db.Group(field)
		}
	} else if len(q.aggregates) > 0 {
		var parts []string
		for _, aggregate := range q.aggregates {
			parts = append(parts, aggregate(db))
		}
		db = db.Select(strings.Join(parts, ","))
	}
	for name, scope := range q.preloads {
		if scope != nil {
			db = db.Preload(name, scope)
		} else {
			db = db.Preload(name)
		}
	}
	return db
}

func (q *Query[T]) All(ctx context.Context) ([]*T, error) {
	rows := make([]*T, 0)
	err := q.db(ctx).Find(&rows).Error
	return rows, err
}
func (q *Query[T]) AllX(ctx context.Context) []*T {
	v, e := q.All(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (q *Query[T]) Only(ctx context.Context) (*T, error) {
	rows, e := q.Clone().Limit(2).All(ctx)
	if e != nil {
		return nil, e
	}
	if len(rows) == 0 {
		return nil, recordNotFound()
	}
	if len(rows) > 1 {
		return nil, errors.New("query returned more than one record")
	}
	return rows[0], nil
}
func (q *Query[T]) OnlyX(ctx context.Context) *T {
	v, e := q.Only(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (q *Query[T]) First(ctx context.Context) (*T, error) {
	rows, err := q.Clone().Limit(1).All(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, recordNotFound()
	}
	return rows[0], nil
}
func (q *Query[T]) Count(ctx context.Context) (int, error) {
	var n int64
	// Counts describe the filtered set rather than the selected page.
	err := q.entity.base(ctx, q.predicates, true).Limit(-1).Offset(-1).Select("*").Count(&n).Error
	return int(n), err
}
func (q *Query[T]) CountX(ctx context.Context) int {
	n, e := q.Count(ctx)
	if e != nil {
		panic(e)
	}
	return n
}
func (q *Query[T]) Exist(ctx context.Context) (bool, error) { n, e := q.Count(ctx); return n > 0, e }
func (q *Query[T]) ExistX(ctx context.Context) bool {
	v, e := q.Exist(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (q *Query[T]) IDs(ctx context.Context) ([]uint32, error) {
	var ids []uint32
	err := q.db(ctx).Pluck("id", &ids).Error
	return ids, err
}
func (q *Query[T]) IDsX(ctx context.Context) []uint32 {
	v, e := q.IDs(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (q *Query[T]) OnlyID(ctx context.Context) (uint32, error) {
	record, err := q.Clone().Select("id").Only(ctx)
	if err != nil {
		return 0, err
	}
	return recordID(record), nil
}
func (q *Query[T]) FirstID(ctx context.Context) (uint32, error) {
	record, err := q.Clone().Select("id").First(ctx)
	if err != nil {
		return 0, err
	}
	return recordID(record), nil
}
func (q *Query[T]) Scan(ctx context.Context, destination any) error {
	return q.db(ctx).Scan(destination).Error
}
func (q *Query[T]) Strings(ctx context.Context) ([]string, error) {
	var values []string
	if len(q.fields) != 1 {
		return nil, errors.New("one field is required")
	}
	err := q.db(ctx).Pluck(q.fields[0], &values).Error
	return values, err
}
func (q *Query[T]) Ints(ctx context.Context) ([]int, error) {
	var values []int
	if len(q.fields) != 1 {
		return nil, errors.New("one field is required")
	}
	err := q.db(ctx).Pluck(q.fields[0], &values).Error
	return values, err
}
func (q *Query[T]) Int64(ctx context.Context) (int64, error) {
	var value int64
	err := q.db(ctx).Scan(&value).Error
	return value, err
}

type Select[T any] struct{ *Query[T] }

type AggregateFunc func(*gorm.DB) string

func Count() AggregateFunc { return func(*gorm.DB) string { return "COUNT(*)" } }
func Sum(field string) AggregateFunc {
	return func(db *gorm.DB) string { return "SUM(" + db.Statement.Quote(clause.Column{Name: field}) + ")" }
}
func As(aggregate AggregateFunc, alias string) AggregateFunc {
	return func(db *gorm.DB) string {
		return aggregate(db) + " AS " + db.Statement.Quote(clause.Column{Name: alias})
	}
}
func Asc(fields ...string) queryscope.Scope {
	return func(db *gorm.DB) *gorm.DB {
		for _, field := range fields {
			db = queryscope.OrderBy(field, false)(db)
		}
		return db
	}
}
func Desc(fields ...string) queryscope.Scope {
	return func(db *gorm.DB) *gorm.DB {
		for _, field := range fields {
			db = queryscope.OrderBy(field, true)(db)
		}
		return db
	}
}

type write[T any] struct {
	entity         *EntityClient[T]
	values         map[string]any
	predicates     []queryscope.Scope
	id             uint32
	conflict       *clause.OnConflict
	conflictValues map[string]any
	err            error
}

func (w *write[T]) set(name string, value any) {
	if w.conflict != nil {
		if w.conflictValues == nil {
			w.conflictValues = make(map[string]any)
		}
		w.conflictValues[name] = value
		return
	}
	w.values[name] = value
}

func (w *write[T]) add(name string, value any) {
	if w.conflict != nil {
		w.set(name, gorm.Expr("? + ?", clause.Column{
			Table: clause.CurrentTable,
			Name:  name,
		}, value))
		return
	}
	w.values[name] = gorm.Expr("? + ?", clause.Column{Name: name}, value)
}
func (w *write[T]) record(ctx context.Context) (*T, error) {
	meta, err := modelSchema[T]()
	if err != nil {
		return nil, err
	}
	record := new(T)
	for name, value := range w.values {
		if field := meta.LookUpField(name); field != nil {
			if err := field.Set(ctx, reflect.ValueOf(record), value); err != nil {
				return nil, fmt.Errorf("set %s: %w", name, err)
			}
		}
	}
	return record, nil
}

func (w *write[T]) prepare(ctx context.Context, create bool) error {
	if privacy.IsDenied(ctx) {
		return ErrDenied
	}
	if w.err != nil {
		return w.err
	}
	meta, err := modelSchema[T]()
	if err != nil {
		return err
	}
	if err := w.prepareTenant(ctx, meta, create); err != nil {
		return err
	}

	now := time.Now()
	if create && meta.LookUpField("created_at") != nil {
		if _, ok := w.values["created_at"]; !ok {
			w.values["created_at"] = now
		}
	}
	if !create && meta.LookUpField("updated_at") != nil {
		if _, ok := w.values["updated_at"]; !ok {
			w.values["updated_at"] = now
		}
	}
	for name, value := range w.values {
		if err := validateModelValue(meta.LookUpField(name), name, value, create); err != nil {
			return err
		}
	}
	if create {
		for _, field := range meta.Fields {
			if !field.NotNull || field.HasDefaultValue || field.AutoIncrement || field.DBName == "" {
				continue
			}
			if _, exists := w.values[field.DBName]; !exists {
				return fmt.Errorf("model field is required: %s", field.DBName)
			}
		}
	}
	return nil
}

func (w *write[T]) mutate(ctx context.Context, op Op, operation func() (any, error)) (any, error) {
	m := &mutation{
		entity: w.entity.entity,
		op:     op,
		id:     w.id,
		fields: w.values,
	}
	var next Mutator = MutateFunc(func(context.Context, Mutation) (Value, error) {
		value, err := operation()
		if row, ok := value.(*T); ok && err == nil {
			m.id = recordID(row)
		}
		return value, err
	})
	for i := len(w.entity.client.hooks) - 1; i >= 0; i-- {
		next = w.entity.client.hooks[i](next)
	}
	return next.Mutate(ctx, m)
}

type Create[T any] struct{ write[T] }

func (b *Create[T]) Save(ctx context.Context) (*T, error) {
	if err := b.prepare(ctx, true); err != nil {
		return nil, err
	}
	record, err := b.record(ctx)
	if err != nil {
		return nil, err
	}
	meta, err := modelSchema[T]()
	if err != nil {
		return nil, err
	}
	columns := make([]string, 0, len(meta.Fields))
	for _, field := range meta.Fields {
		if field.DBName == "" {
			continue
		}
		_, explicit := b.values[field.DBName]
		if explicit || (field.HasDefaultValue && !field.PrimaryKey) {
			columns = append(columns, field.DBName)
		}
	}
	_, err = b.mutate(ctx, OpCreate, func() (any, error) {
		db := b.entity.client.db.WithContext(ctx).Select(columns)
		if b.conflict != nil {
			conflict := *b.conflict
			if !conflict.DoNothing {
				assignments := make(map[string]any)
				if conflict.UpdateAll {
					for _, column := range columns {
						if field := meta.LookUpField(column); field != nil && !field.PrimaryKey {
							assignments[column] = clause.Column{
								Table: "excluded",
								Name:  column,
							}
						}
					}
				}
				for name, value := range b.conflictValues {
					assignments[name] = value
				}
				if len(assignments) == 0 {
					conflict.DoNothing = true
				} else {
					update := write[T]{
						entity: b.entity,
						values: assignments,
					}
					values, serializeErr := update.serializedValues(ctx)
					if serializeErr != nil {
						return nil, serializeErr
					}
					conflict.UpdateAll = false
					conflict.DoUpdates = clause.Assignments(values)
				}
			}
			db = db.Clauses(conflict)
		}
		return record, db.Create(record).Error
	})
	return record, err
}
func (b *Create[T]) SaveX(ctx context.Context) *T {
	v, e := b.Save(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (b *Create[T]) Exec(ctx context.Context) error { _, err := b.Save(ctx); return err }
func (b *Create[T]) ExecX(ctx context.Context) {
	if e := b.Exec(ctx); e != nil {
		panic(e)
	}
}
func (b *Create[T]) OnConflictColumns(columns ...string) *Create[T] {
	c := clause.OnConflict{}
	for _, column := range columns {
		c.Columns = append(c.Columns, clause.Column{Name: column})
	}
	b.conflict = &c
	return b
}
func (b *Create[T]) UpdateNewValues() *Create[T] {
	if b.conflict == nil {
		b.conflict = &clause.OnConflict{}
	}
	b.conflict.UpdateAll = true
	return b
}
func (b *Create[T]) Ignore() *Create[T] {
	if b.conflict == nil {
		b.conflict = &clause.OnConflict{}
	}
	b.conflict.DoNothing = true
	return b
}

type CreateBulk[T any] struct {
	entity   *EntityClient[T]
	builders []*Create[T]
	ignore   bool
}

func (b *CreateBulk[T]) Save(ctx context.Context) ([]*T, error) {
	rows := make([]*T, 0, len(b.builders))
	err := b.entity.client.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		client := NewClient(db)
		client.hooks = b.entity.client.hooks
		entity := &EntityClient[T]{
			client:    client,
			entity:    b.entity.entity,
			dataScope: b.entity.dataScope,
		}
		for _, builder := range b.builders {
			copy := *builder
			copy.entity = entity
			if b.ignore {
				copy.Ignore()
			}
			row, err := copy.Save(ctx)
			if err != nil {
				return err
			}
			rows = append(rows, row)
		}
		return nil
	})
	return rows, err
}
func (b *CreateBulk[T]) SaveX(ctx context.Context) []*T {
	v, e := b.Save(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (b *CreateBulk[T]) Exec(ctx context.Context) error { _, e := b.Save(ctx); return e }
func (b *CreateBulk[T]) ExecX(ctx context.Context) {
	if e := b.Exec(ctx); e != nil {
		panic(e)
	}
}
func (b *CreateBulk[T]) Ignore() *CreateBulk[T] { b.ignore = true; return b }

type Update[T any] struct{ write[T] }

func (b *Update[T]) Where(predicates ...queryscope.Scope) *Update[T] {
	b.predicates = append(b.predicates, predicates...)
	return b
}
func (b *Update[T]) Save(ctx context.Context) (int, error) {
	if err := b.prepare(ctx, false); err != nil {
		return 0, err
	}
	values, err := b.serializedValues(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	_, err = b.mutate(ctx, OpUpdate, func() (any, error) {
		result := b.entity.base(ctx, b.predicates, false).Updates(values)
		n = int(result.RowsAffected)
		return n, result.Error
	})
	return n, err
}
func (b *Update[T]) SaveX(ctx context.Context) int {
	v, e := b.Save(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (b *Update[T]) Exec(ctx context.Context) error { _, e := b.Save(ctx); return e }
func (b *Update[T]) ExecX(ctx context.Context) {
	if e := b.Exec(ctx); e != nil {
		panic(e)
	}
}

type UpdateOne[T any] struct{ write[T] }

func (b *UpdateOne[T]) Where(predicates ...queryscope.Scope) *UpdateOne[T] {
	b.predicates = append(b.predicates, predicates...)
	return b
}
func (b *UpdateOne[T]) Save(ctx context.Context) (*T, error) {
	if err := b.prepare(ctx, false); err != nil {
		return nil, err
	}
	values, err := b.serializedValues(ctx)
	if err != nil {
		return nil, err
	}
	_, err = b.mutate(ctx, OpUpdateOne, func() (any, error) {
		result := b.entity.base(ctx, b.predicates, false).Updates(values)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, recordNotFound()
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return b.entity.Query().Where(b.predicates...).Only(ctx)
}
func (b *UpdateOne[T]) SaveX(ctx context.Context) *T {
	v, e := b.Save(ctx)
	if e != nil {
		panic(e)
	}
	return v
}
func (b *UpdateOne[T]) Exec(ctx context.Context) error { _, e := b.Save(ctx); return e }
func (b *UpdateOne[T]) ExecX(ctx context.Context) {
	if e := b.Exec(ctx); e != nil {
		panic(e)
	}
}

type Delete[T any] struct {
	entity     *EntityClient[T]
	predicates []queryscope.Scope
}

func (b *Delete[T]) Where(predicates ...queryscope.Scope) *Delete[T] {
	b.predicates = append(b.predicates, predicates...)
	return b
}
func (b *Delete[T]) Exec(ctx context.Context) (int, error) {
	w := write[T]{entity: b.entity}
	var n int
	_, err := w.mutate(ctx, OpDelete, func() (any, error) {
		db := b.entity.base(ctx, b.predicates, false)
		if len(b.predicates) == 0 {
			db = db.Session(&gorm.Session{AllowGlobalUpdate: true})
		}
		result := db.Delete(new(T))
		n = int(result.RowsAffected)
		return n, result.Error
	})
	return n, err
}
func (b *Delete[T]) ExecX(ctx context.Context) int {
	v, e := b.Exec(ctx)
	if e != nil {
		panic(e)
	}
	return v
}

type DeleteOne[T any] struct{ Delete[T] }

func (b *DeleteOne[T]) Exec(ctx context.Context) error {
	n, e := b.Delete.Exec(ctx)
	if e != nil {
		return e
	}
	if n == 0 {
		return recordNotFound()
	}
	return nil
}
func (b *DeleteOne[T]) ExecX(ctx context.Context) {
	if e := b.Exec(ctx); e != nil {
		panic(e)
	}
}

type Op uint8

const (
	OpCreate Op = 1 << iota
	OpUpdate
	OpUpdateOne
	OpDelete
	OpDeleteOne
)

func (op Op) String() string {
	switch op {
	case OpCreate:
		return "OpCreate"
	case OpUpdate:
		return "OpUpdate"
	case OpUpdateOne:
		return "OpUpdateOne"
	case OpDelete:
		return "OpDelete"
	case OpDeleteOne:
		return "OpDeleteOne"
	}
	return fmt.Sprintf("Op(%d)", op)
}

type Value = any
type Mutation interface {
	Type() string
	Op() Op
	Fields() []string
	Field(string) (any, bool)
}
type Mutator interface {
	Mutate(context.Context, Mutation) (Value, error)
}
type MutateFunc func(context.Context, Mutation) (Value, error)

func (f MutateFunc) Mutate(ctx context.Context, m Mutation) (Value, error) { return f(ctx, m) }

type Hook func(Mutator) Mutator
type mutation struct {
	entity string
	op     Op
	id     uint32
	fields map[string]any
}

func (m *mutation) Type() string       { return m.entity }
func (m *mutation) Op() Op             { return m.op }
func (m *mutation) ID() (uint32, bool) { return m.id, m.id != 0 }
func (m *mutation) Fields() []string {
	fields := make([]string, 0, len(m.fields))
	for field := range m.fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}
func (m *mutation) Field(name string) (any, bool) {
	value, exists := m.fields[name]
	return value, exists
}

func validateModelValue(field *schema.Field, name string, value any, create bool) error {
	if field == nil {
		return fmt.Errorf("unknown model field: %s", name)
	}
	if !create && field.StructField.Tag.Get("orm_immutable") == "true" {
		return fmt.Errorf("model field is immutable: %s", name)
	}
	if value == nil {
		if field.NotNull && (!create || !field.HasDefaultValue) {
			return fmt.Errorf("model field is required: %s", name)
		}
		return nil
	}
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			break
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.String {
		text := v.String()
		minLen, _ := strconv.Atoi(field.StructField.Tag.Get("orm_min_len"))
		maxLen, _ := strconv.Atoi(field.StructField.Tag.Get("orm_max_len"))
		n := utf8.RuneCountInString(text)
		if n < minLen || (maxLen > 0 && n > maxLen) {
			return fmt.Errorf("model field length is invalid: %s", name)
		}
		if allowed := field.StructField.Tag.Get("orm_enum"); allowed != "" {
			valid := false
			for _, candidate := range strings.Split(allowed, ",") {
				if text == candidate {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("model enum value is invalid: %s", name)
			}
		}
	}
	return nil
}

func (w *write[T]) prepareTenant(ctx context.Context, meta *schema.Schema, create bool) error {
	if meta.LookUpField("tenant_id") != nil {
		vc, ok := viewer.FromContext(ctx)
		if !ok {
			return ErrMissingViewer
		}
		if !vc.IsSystemContext() && !vc.IsPlatformContext() {
			if supplied, ok := w.values["tenant_id"]; ok && !create {
				if supplied == nil {
					return ErrCrossTenant
				}
				v := reflect.ValueOf(supplied)
				for v.Kind() == reflect.Pointer {
					if v.IsNil() {
						return ErrCrossTenant
					}
					v = v.Elem()
				}
				if !v.IsValid() || (v.Kind() != reflect.Uint32 && v.Kind() != reflect.Uint64) || v.Uint() != vc.TenantID() {
					return ErrCrossTenant
				}
			}
			if create {
				w.values["tenant_id"] = uint32(vc.TenantID())
			}
		}
	}
	return nil
}
