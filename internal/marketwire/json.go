package marketwire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/hmlax/muskdeer/internal/marketbook"
)

const maximumPayloadBytes = 1024 * 1024

type payloadError struct{ cause error }

func (*payloadError) Error() string { return MessageInvalidPayload }
func (e *payloadError) Unwrap() error {
	return errors.Join(ErrInvalidPayload, e.cause)
}

// decode keeps business values in the concrete wire struct. The first pass
// checks only token shape/schema: no object-to-map conversion or numeric float.
func decode[T any](ctx context.Context, data []byte) (T, error) {
	var output T
	if ctx == nil || len(data) == 0 || len(data) > maximumPayloadBytes {
		return output, ErrInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return output, err
	}
	tokens := json.NewDecoder(bytes.NewReader(data))
	tokens.UseNumber()
	if err := schemaValue(ctx, tokens, reflect.TypeFor[T](), 0); err != nil {
		return output, &payloadError{cause: err}
	}
	if _, err := tokens.Token(); !errors.Is(err, io.EOF) {
		return output, ErrInvalidPayload
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return output, &payloadError{cause: err}
	}
	return output, ctx.Err()
}

func schemaValue(ctx context.Context, decoder *json.Decoder, target reflect.Type, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > 64 {
		return ErrInvalidPayload
	}
	nullable := target.Kind() == reflect.Pointer || target.Kind() == reflect.Slice
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if nullable {
			return nil
		}
		return ErrInvalidPayload
	}
	if delimiter, ok := token.(json.Delim); ok {
		switch delimiter {
		case '{':
			return schemaObject(ctx, decoder, target, depth)
		case '[':
			return schemaArray(ctx, decoder, target, depth)
		default:
			return ErrInvalidPayload
		}
	}
	switch target.Kind() {
	case reflect.String:
		if _, ok := token.(string); ok {
			return nil
		}
	case reflect.Int64:
		if _, ok := token.(json.Number); ok {
			return nil
		}
	}
	return ErrInvalidPayload
}

func schemaObject(ctx context.Context, decoder *json.Decoder, target reflect.Type, depth int) error {
	if target.Kind() != reflect.Struct {
		return ErrInvalidPayload
	}
	fields := make(map[string]reflect.Type, target.NumField())
	for index := range target.NumField() {
		field := target.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.PkgPath == "" && name != "" && name != "-" {
			fields[name] = field.Type
		}
	}
	seen := make(map[string]struct{}, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return ErrInvalidPayload
		}
		field, known := fields[key]
		if _, duplicate := seen[key]; duplicate || !known {
			return ErrInvalidPayload
		}
		seen[key] = struct{}{}
		if err := schemaValue(ctx, decoder, field, depth+1); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return ErrInvalidPayload
	}
	return nil
}

func schemaArray(ctx context.Context, decoder *json.Decoder, target reflect.Type, depth int) error {
	if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
		return ErrInvalidPayload
	}
	count := 0
	for decoder.More() {
		count++
		if count > marketbook.MaximumLevels {
			return ErrInvalidPayload
		}
		if err := schemaValue(ctx, decoder, target.Elem(), depth+1); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim(']') {
		return ErrInvalidPayload
	}
	return nil
}
