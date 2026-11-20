package store

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JSONField struct {
	Name  string
	Value any
}
type jsonPatch struct {
	column string
	fields []JSONField
}

// PatchJSON updates selected document fields in one SQL statement. Values are
// serialized only at the storage boundary, preserving false, zero and JSON null.
func PatchJSON(column string, fields ...JSONField) UpdateModifier {
	return func(values map[string]any) {
		values[column] = jsonPatch{
			column: column,
			fields: fields,
		}
	}
}
func (p jsonPatch) Build(builder clause.Builder) {
	statement, ok := builder.(*gorm.Statement)
	if !ok {
		_ = builder.AddError(fmt.Errorf("JSON patch requires a GORM statement"))
		return
	}
	column := clause.Column{Name: p.column}
	var expression clause.Expression = clause.Expr{
		SQL:  "COALESCE(?, '{}')",
		Vars: []any{column},
	}
	if statement.Name() == "postgres" {
		expression = clause.Expr{
			SQL:  "COALESCE(?::jsonb, '{}'::jsonb)",
			Vars: []any{column},
		}
	}
	for _, field := range p.fields {
		encoded, err := json.Marshal(field.Value)
		if err != nil {
			_ = builder.AddError(fmt.Errorf("encode JSON field %s: %w", field.Name, err))
			return
		}
		switch statement.Name() {
		case "postgres":
			expression = gorm.Expr("jsonb_set(?, ?, ?::jsonb, true)", expression, "{"+field.Name+"}", string(encoded))
		case "mysql":
			expression = gorm.Expr("JSON_SET(?, ?, CAST(? AS JSON))", expression, "$."+field.Name, string(encoded))
		case "sqlite":
			expression = gorm.Expr("JSON_SET(?, ?, JSON(?))", expression, "$."+field.Name, string(encoded))
		default:
			_ = builder.AddError(fmt.Errorf("unsupported JSON patch dialect: %s", statement.Name()))
			return
		}
	}
	expression.Build(builder)
}
