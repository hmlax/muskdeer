// Package adminopenapi describes the existing public management HTTP boundary.
// Business payload schemas come from the same typed Proto messages as delivery.
package adminopenapi

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	integerStringFormat = "muskdeer-int64"
	integerPattern      = `^(0|-?[1-9][0-9]*)$`
	positivePattern     = `^[1-9][0-9]*$`
)

// Document is an OpenAPI 3.1 document with explicit metadata types.
type Document struct {
	OpenAPI    string               `json:"openapi"`
	Info       Info                 `json:"info"`
	Paths      map[string]*PathItem `json:"paths"`
	Components Components           `json:"components"`
}

// Info explains the scope and the fixed contract revision.
type Info struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// Components contains only public HTTP bodies and session scheme metadata.
type Components struct {
	Schemas         map[string]*Schema        `json:"schemas"`
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes"`
}

// SecurityScheme describes one of the mutually exclusive deployment cookies.
type SecurityScheme struct {
	Type        string `json:"type"`
	In          string `json:"in"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// PathItem exposes the methods currently implemented by Hertz.
type PathItem struct {
	Get  *Operation `json:"get,omitempty"`
	Post *Operation `json:"post,omitempty"`
}

// Operation records feature gates, authority and the HTTP success alternatives.
type Operation struct {
	OperationID string              `json:"operationId"`
	Summary     string              `json:"summary"`
	Description string              `json:"description"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	RequestBody *RequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]Response `json:"responses"`
	Security    []Security          `json:"security"`
	Feature     string              `json:"x-muskdeer-feature"`
	MinimumRole string              `json:"x-muskdeer-minimum-role"`
}

// Security is an OpenAPI alternatives entry; both cookies are never required.
type Security map[string][]string

// Parameter describes one non-repeated path, query or header value.
type Parameter struct {
	Name        string  `json:"name"`
	In          string  `json:"in"`
	Required    bool    `json:"required,omitempty"`
	Description string  `json:"description,omitempty"`
	Schema      *Schema `json:"schema"`
}

// RequestBody describes the only supported management write content type.
type RequestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]MediaType `json:"content"`
}

// MediaType binds JSON to a typed schema.
type MediaType struct {
	Schema *Schema `json:"schema"`
}

// Response describes a complete JSON reply or the bodyless logout response.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
	Headers     map[string]Header    `json:"headers,omitempty"`
}

// Header documents response headers without exposing cookie values.
type Header struct {
	Description string  `json:"description"`
	Schema      *Schema `json:"schema"`
}

// Schema is the JSON Schema subset needed by the public typed boundary.
// Dictionaries hold schema metadata, never converted business payloads.
type Schema struct {
	Ref                  string             `json:"$ref,omitempty"`
	Type                 []string           `json:"type,omitempty"`
	Description          string             `json:"description,omitempty"`
	Format               string             `json:"format,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	Required             []string           `json:"required,omitempty"`
	AdditionalProperties *bool              `json:"additionalProperties,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	Minimum              *int64             `json:"minimum,omitempty"`
	Maximum              *int64             `json:"maximum,omitempty"`
	MinLength            int                `json:"minLength,omitempty"`
	MaxLength            int                `json:"maxLength,omitempty"`
}

type schemaBuilder struct {
	schemas map[string]*Schema
	err     error
}

// Build constructs a fresh document without files, network or mutable globals.
func Build() (*Document, error) {
	builder := &schemaBuilder{
		schemas: manualSchemas(),
	}
	document := &Document{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:   "Muskdeer management HTTP API",
			Version: "P07.56",
			Description: "Existing management boundary; simulation acceptance does not authorize production trading. " +
				"All replies use Cache-Control: no-store. Fixed Host is enforced. Writes require the configured exact Origin. " +
				"Feature-disabled routes return 404. Public JSON uses snake_case and decimal strings for every int64 and financial decimal. " +
				"Money/price/quantity/rate precision, authorization, duplicate JSON names, byte limits and cross-field invariants are also enforced by delivery/domain validation; schema validation alone is insufficient. " +
				"No server address or credential is included. The two cookie schemes are deployment alternatives. Read observations never grant execution ownership or risk permission.",
		},
		Paths: make(map[string]*PathItem),
		Components: Components{
			Schemas: builder.schemas,
			SecuritySchemes: map[string]SecurityScheme{
				"secureSession": {
					Type:        "apiKey",
					In:          "cookie",
					Name:        "__Host-muskdeer_session",
					Description: "HTTPS deployment: HttpOnly, Secure, SameSite=Strict, Path=/; opaque secret, never a business JSON field.",
				},
				"localSession": {
					Type:        "apiKey",
					In:          "cookie",
					Name:        "muskdeer_session",
					Description: "Explicit local HTTP mode only: HttpOnly, SameSite=Strict, Path=/.",
				},
			},
		},
	}
	for _, definition := range definitions() {
		item := document.Paths[definition.path]
		if item == nil {
			item = &PathItem{}
			document.Paths[definition.path] = item
		}
		operation := builder.operation(definition)
		if definition.write {
			item.Post = operation
		} else {
			item.Get = operation
		}
	}
	if builder.err != nil {
		return nil, builder.err
	}
	return document, nil
}

// Generate emits deterministic bytes; encoding/json sorts metadata map keys.
func Generate() ([]byte, error) {
	document, err := Build()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode management OpenAPI: %w", err)
	}
	return append(data, '\n'), nil
}

func object(properties map[string]*Schema, required ...string) *Schema {
	closed := false
	return &Schema{
		Type:                 []string{"object"},
		Properties:           properties,
		Required:             required,
		AdditionalProperties: &closed,
	}
}

func text() *Schema {
	return &Schema{
		Type: []string{"string"},
	}
}

func stringsEnum(values ...string) *Schema {
	return &Schema{
		Type: []string{"string"},
		Enum: values,
	}
}

func int64String(positive bool) *Schema {
	pattern := integerPattern
	if positive {
		pattern = positivePattern
	}
	return &Schema{
		Type:        []string{"string"},
		Format:      integerStringFormat,
		Pattern:     pattern,
		MaxLength:   20,
		Description: "Canonical base-10 signed int64 string; bounded to [-9223372036854775808,9223372036854775807]. Never parse through a binary floating-point number.",
	}
}

func reference(name string) *Schema {
	return &Schema{
		Ref: "#/components/schemas/" + name,
	}
}

func nullable(schema *Schema) *Schema {
	return &Schema{
		AnyOf: []*Schema{
			schema,
			{
				Type: []string{"null"},
			},
		},
	}
}

func (b *schemaBuilder) message(message protoreflect.MessageDescriptor, output bool) *Schema {
	name := string(message.FullName())
	if !output {
		name += ".Input"
	}
	if b.schemas[name] != nil {
		return reference(name)
	}
	schema := object(make(map[string]*Schema))
	b.schemas[name] = schema
	for i := 0; i < message.Fields().Len(); i++ {
		field := message.Fields().Get(i)
		fieldName := string(field.Name())
		schema.Properties[fieldName] = b.field(field, output)
		if output && !field.HasOptionalKeyword() && field.ContainingOneof() == nil {
			schema.Required = append(schema.Required, fieldName)
		}
	}
	// The only public union is StrategyParameters; exactly one typed template.
	for i := 0; i < message.Oneofs().Len(); i++ {
		oneof := message.Oneofs().Get(i)
		if oneof.IsSynthetic() {
			continue
		}
		for j := 0; j < oneof.Fields().Len(); j++ {
			schema.OneOf = append(schema.OneOf, &Schema{
				Required: []string{string(oneof.Fields().Get(j).Name())},
			})
		}
	}
	if !output {
		configureInput(message.FullName(), schema)
	}
	return reference(name)
}

func (b *schemaBuilder) field(field protoreflect.FieldDescriptor, output bool) *Schema {
	var schema *Schema
	switch field.Kind() {
	case protoreflect.StringKind:
		schema = text()
	case protoreflect.BoolKind:
		schema = &Schema{
			Type: []string{"boolean"},
		}
	case protoreflect.Int32Kind:
		minimum, maximum := int64(-2147483648), int64(2147483647)
		schema = &Schema{
			Type:    []string{"integer"},
			Minimum: &minimum,
			Maximum: &maximum,
		}
	case protoreflect.Int64Kind:
		schema = int64String(false)
	case protoreflect.MessageKind:
		schema = b.message(field.Message(), output)
	default:
		b.err = fmt.Errorf("unsupported public field %s (%s)", field.FullName(), field.Kind())
		return nil
	}
	if field.IsMap() {
		b.err = fmt.Errorf("untyped public map is unsupported: %s", field.FullName())
		return nil
	}
	if field.IsList() {
		return &Schema{
			Type:  []string{"array"},
			Items: schema,
		}
	}
	if output && field.Kind() == protoreflect.MessageKind && field.ContainingOneof() == nil {
		return nullable(schema)
	}
	return schema
}
