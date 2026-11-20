// Package predicatex builds generated relationship query scopes.
package predicatex

import (
	"fmt"
	"sync"

	"github.com/hmlax/muskdeer/operator/internal/data/store/queryscope"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

var schemas sync.Map

// Relation constrains a generated relation through model-bound queries.
func Relation[Source, Target any](name string, predicates ...queryscope.Scope) queryscope.Scope {
	return func(db *gorm.DB) *gorm.DB {
		source, err := schema.Parse(new(Source), &schemas, schema.NamingStrategy{SingularTable: true})
		if err != nil {
			_ = db.AddError(err)
			return db
		}
		relation := source.Relationships.Relations[name]
		if relation == nil || len(relation.References) != 1 {
			_ = db.AddError(fmt.Errorf("unknown model association: %s", name))
			return db
		}
		ref := relation.References[0]
		query := db.Session(&gorm.Session{NewDB: true}).Model(new(Target))
		for _, predicate := range predicates {
			query = predicate(query)
		}
		sourceField, targetField := ref.ForeignKey.DBName, ref.PrimaryKey.DBName
		if ref.OwnPrimaryKey {
			sourceField, targetField = ref.PrimaryKey.DBName, ref.ForeignKey.DBName
		}
		query = query.Select(targetField)
		return db.Where(clause.Expr{
			SQL: "? IN (?)",
			Vars: []any{clause.Column{
				Table: clause.CurrentTable,
				Name:  sourceField,
			}, query},
		})
	}
}
