// Package testopenapi validates actual test HTTP payloads against downloaded
// contract bytes. It is never imported by a business process.
package testopenapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/hmlax/muskdeer/internal/adminopenapi"
)

const documentURL = "https://muskdeer.invalid/openapi.json"

// Contract owns its offline compiler and explicitly typed operation metadata.
// Tests use it sequentially; it spawns no work or external loaders.
type Contract struct {
	compiler *jsonschema.Compiler
	document adminopenapi.Document
}

// New accepts document bytes received through actual HTTP, without loading files.
func New(data []byte) (*Contract, error) {
	var document adminopenapi.Document
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if document.OpenAPI != "3.1.0" || len(document.Paths) == 0 {
		return nil, fmt.Errorf("missing OpenAPI 3.1 operations")
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyLoader{})
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{
		Name:     "muskdeer-int64",
		Validate: validateIntegerString,
	})
	if err := compiler.AddResource(documentURL, value); err != nil {
		return nil, err
	}
	return &Contract{
		compiler: compiler,
		document: document,
	}, nil
}

// ValidateResponse compiles the declared operation/status schema and checks raw
// wire JSON. The validator's generic JSON value is not used to copy or translate
// business objects; native tests independently decode typed Proto replies.
func (c *Contract) ValidateResponse(method, path string, status int, data []byte) error {
	item := c.document.Paths[path]
	if item == nil {
		return fmt.Errorf("undocumented path %s", path)
	}
	var operation *adminopenapi.Operation
	switch method {
	case "GET":
		operation = item.Get
	case "POST":
		operation = item.Post
	}
	if operation == nil {
		return fmt.Errorf("undocumented method %s", method)
	}
	response, ok := operation.Responses[strconv.Itoa(status)]
	if !ok {
		return fmt.Errorf("undocumented response status %d", status)
	}
	if len(response.Content) == 0 {
		if len(data) != 0 {
			return fmt.Errorf("bodyless response had data")
		}
		return nil
	}
	if response.Content["application/json"].Schema == nil {
		return fmt.Errorf("missing JSON response schema")
	}
	location := documentURL + "#/paths/" + pointerToken(path) + "/" + strings.ToLower(method) + "/responses/" + strconv.Itoa(status) + "/content/application~1json/schema"
	schema, err := c.compiler.Compile(location)
	if err != nil {
		return err
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return schema.Validate(value)
}

func pointerToken(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func validateIntegerString(value any) error {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	if err != nil || strconv.FormatInt(integer, 10) != text {
		return fmt.Errorf("invalid canonical int64 string")
	}
	return nil
}

type denyLoader struct{}

func (denyLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external schema loading prohibited: %s", location)
}
