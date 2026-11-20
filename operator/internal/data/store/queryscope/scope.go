// Package queryscope defines native GORM scopes used by generated model predicates.
package queryscope

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Scope is a model-bound query transformation.
type Scope = func(*gorm.DB) *gorm.DB

// Equal compares a generated field with a value.
func Equal(field string, value any) Scope {
	return Where(clause.Eq{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// NotEqual excludes a generated field value.
func NotEqual(field string, value any) Scope {
	return Where(clause.Neq{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// Greater compares a generated field with a lower bound.
func Greater(field string, value any) Scope {
	return Where(clause.Gt{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// GreaterEqual compares a generated field with an inclusive lower bound.
func GreaterEqual(field string, value any) Scope {
	return Where(clause.Gte{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// Less compares a generated field with an upper bound.
func Less(field string, value any) Scope {
	return Where(clause.Lt{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// LessEqual compares a generated field with an inclusive upper bound.
func LessEqual(field string, value any) Scope {
	return Where(clause.Lte{
		Column: clause.Column{Name: field},
		Value:  value,
	})
}

// In restricts a generated field to the supplied values.
func In(field string, values ...any) Scope {
	return Where(clause.IN{
		Column: clause.Column{Name: field},
		Values: values,
	})
}

// NotIn excludes the supplied generated field values.
func NotIn(field string, values ...any) Scope { return Not(In(field, values...)) }

// IsNull restricts a generated field to NULL.
func IsNull(field string) Scope { return Equal(field, nil) }

// NotNull excludes NULL generated field values.
func NotNull(field string) Scope { return NotEqual(field, nil) }

// Where applies a typed GORM expression.
func Where(expression clause.Expression) Scope {
	return func(db *gorm.DB) *gorm.DB { return db.Where(expression) }
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}
func like(field, pattern string, fold bool) Scope {
	if fold {
		pattern = strings.ToLower(pattern)
	}
	return func(db *gorm.DB) *gorm.DB {
		// SQLite requires an explicit escape clause; MySQL/PostgreSQL accept it too.
		expression := "? LIKE ? ESCAPE '\\'"
		if db.Name() == "mysql" {
			expression = "? LIKE ? ESCAPE '\\\\'"
		}
		if fold {
			expression = "LOWER(" + expression
			expression = strings.Replace(expression, " LIKE", ") LIKE", 1)
		}
		return db.Where(clause.Expr{
			SQL:  expression,
			Vars: []any{clause.Column{Name: field}, pattern},
		})
	}
}

// Contains applies an escaped substring filter.
func Contains(field, value string) Scope { return like(field, "%"+escapeLike(value)+"%", false) }

// ContainsFold applies an escaped case-insensitive substring filter.
func ContainsFold(field, value string) Scope { return like(field, "%"+escapeLike(value)+"%", true) }

// HasPrefix applies an escaped prefix filter.
func HasPrefix(field, value string) Scope { return like(field, escapeLike(value)+"%", false) }

// HasSuffix applies an escaped suffix filter.
func HasSuffix(field, value string) Scope { return like(field, "%"+escapeLike(value), false) }

// EqualFold compares escaped text without case sensitivity.
func EqualFold(field, value string) Scope { return like(field, escapeLike(value), true) }

func group(scopes []Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		child := db.Session(&gorm.Session{NewDB: true})
		for _, scope := range scopes {
			child = scope(child)
		}
		return child
	}
}

// And combines all supplied query predicates.
func And(scopes ...Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		child := group(scopes)(db)
		if child.Error != nil {
			_ = db.AddError(child.Error)
			return db
		}
		return db.Where(child)
	}
}

// Or combines alternative query predicates.
func Or(scopes ...Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		if len(scopes) == 0 {
			return db.Where("1 = 0")
		}
		child := db.Session(&gorm.Session{NewDB: true})
		for _, scope := range scopes {
			branch := group([]Scope{scope})(db)
			if branch.Error != nil {
				_ = db.AddError(branch.Error)
				return db
			}
			child = child.Or(branch)
		}
		return db.Where(child)
	}
}

// Not negates a query predicate.
func Not(scope Scope) Scope {
	return func(db *gorm.DB) *gorm.DB {
		child := group([]Scope{scope})(db)
		if child.Error != nil {
			_ = db.AddError(child.Error)
			return db
		}
		return db.Not(child)
	}
}

// OrderBy builds a quoted column order; input is never treated as SQL text.
func OrderBy(field string, descending bool) Scope {
	return func(db *gorm.DB) *gorm.DB {
		return db.Order(clause.OrderByColumn{
			Column: clause.Column{Name: field},
			Desc:   descending,
		})
	}
}
