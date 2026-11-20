package store

import (
	"context"
	"fmt"
	"reflect"

	"gorm.io/gorm/clause"
)

type UpdateModifier func(map[string]any)

func (b *Update[T]) Modify(modifiers ...UpdateModifier) *Update[T] {
	for _, modifier := range modifiers {
		modifier(b.values)
	}
	return b
}
func (b *UpdateOne[T]) Modify(modifiers ...UpdateModifier) *UpdateOne[T] {
	for _, modifier := range modifiers {
		modifier(b.values)
	}
	return b
}
func SetNull(fields ...string) UpdateModifier {
	return func(values map[string]any) {
		for _, field := range fields {
			values[field] = nil
		}
	}
}

func (w *write[T]) serializedValues(ctx context.Context) (map[string]any, error) {
	meta, err := modelSchema[T]()
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(w.values))
	for name, value := range w.values {
		field := meta.LookUpField(name)
		if field == nil {
			return nil, fmt.Errorf("unknown update column: %s", name)
		}
		if _, expression := value.(clause.Expression); expression {
			values[name] = value
			continue
		}
		if field.Serializer != nil && value != nil {
			// Serializer values are produced from the original typed JSON value.
			value, err = field.Serializer.Value(ctx, field, reflect.ValueOf(new(T)), value)
			if err != nil {
				return nil, err
			}
		}
		values[name] = value
	}
	return values, nil
}
