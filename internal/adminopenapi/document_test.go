package adminopenapi

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	strategyv1 "github.com/hmlax/muskdeer/api/strategy/v1"
)

const documentURL = "https://muskdeer.invalid/openapi.json"

func TestDocumentCoversActualManagementRoutes(t *testing.T) {
	document, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "../delivery/adminhttp/routes.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "GET" && selector.Sel.Name != "POST") {
			return true
		}
		literal, ok := call.Args[0].(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatalf("invalid management registration: %s", literal.Value)
		}
		// HTML/assets and the authenticated document download are outside the
		// /api/v1 business contract, and have dedicated delivery tests.
		if !strings.HasPrefix(path, "/api/v1/") {
			return true
		}
		for _, part := range strings.Split(path, "/") {
			if strings.HasPrefix(part, ":") {
				path = strings.ReplaceAll(path, part, "{"+part[1:]+"}")
			}
		}
		want[strings.ToLower(selector.Sel.Name)+" "+path] = true
		return true
	})
	seenIDs := make(map[string]bool)
	count := 0
	for path, item := range document.Paths {
		for method, operation := range map[string]*Operation{"get": item.Get, "post": item.Post} {
			if operation == nil {
				continue
			}
			key := method + " " + path
			if !want[key] || seenIDs[operation.OperationID] || operation.OperationID == "" {
				t.Fatalf("unregistered or duplicate operation: %s", key)
			}
			delete(want, key)
			seenIDs[operation.OperationID] = true
			count++
		}
	}
	if count != len(definitions()) || len(want) != 0 {
		t.Fatalf("documented %d routes; missing %v", count, want)
	}
}

type denyLoader struct{}

func (denyLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external schema loading prohibited: %s", location)
}

func offlineCompiler() *jsonschema.Compiler {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyLoader{})
	compiler.AssertFormat()
	compiler.RegisterFormat(&jsonschema.Format{
		Name: integerStringFormat,
		Validate: func(value any) error {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			integer, err := strconv.ParseInt(text, 10, 64)
			if err != nil || strconv.FormatInt(integer, 10) != text {
				return fmt.Errorf("noncanonical or out-of-range int64 string")
			}
			return nil
		},
	})
	return compiler
}

func TestGeneratedDocumentMatchesFileAndOfficialSchema(t *testing.T) {
	generated, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, generated) {
		t.Fatal("OpenAPI differs; run make openapi-generate")
	}
	compiler := offlineCompiler()
	data, err := os.ReadFile("../../third_party/openapi/schema-2022-10-07.json")
	if err != nil {
		t.Fatal(err)
	}
	addJSON(t, compiler, "https://spec.openapis.org/oas/3.1/schema/2022-10-07", data)
	schema, err := compiler.Compile("https://spec.openapis.org/oas/3.1/schema/2022-10-07")
	if err != nil {
		t.Fatal(err)
	}
	validateJSON(t, schema, generated, true)
}

func TestResponseSchemaPreservesProtoJSONPresenceAndStringIntegers(t *testing.T) {
	compiler := documentCompiler(t)
	message := &strategyv1.GetStrategyReply{
		Strategy: &strategyv1.Strategy{
			Id:                  9007199254740993,
			RuntimeOwnerEpoch:   9223372036854775807,
			ActiveConfigVersion: proto.Int64(4),
		},
	}
	schema := compileComponent(t, compiler, string(message.ProtoReflect().Descriptor().FullName()))
	wire, err := (protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: true,
	}).Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	validateJSON(t, schema, wire, true)
	validateJSON(t, schema, bytes.Replace(wire, []byte(`"9007199254740993"`), []byte(`9007199254740993`), 1), false)
	validateJSON(t, schema, bytes.Replace(wire, []byte(`"9223372036854775807"`), []byte(`"9223372036854775808"`), 1), false)
	validateJSON(t, schema, bytes.Replace(wire, []byte(`"active_config_version":"4"`), []byte(`"active_config_version":null`), 1), false)
	validateJSON(t, schema, []byte(`{"strategy":null,"requested_config":null,"active_config":null,"secret":"hidden"}`), false)
}

func documentCompiler(t *testing.T) *jsonschema.Compiler {
	t.Helper()
	data, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	compiler := offlineCompiler()
	addJSON(t, compiler, documentURL, data)
	return compiler
}

func addJSON(t *testing.T, compiler *jsonschema.Compiler, location string, data []byte) {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource(location, value); err != nil {
		t.Fatal(err)
	}
}

func compileComponent(t *testing.T, compiler *jsonschema.Compiler, name string) *jsonschema.Schema {
	t.Helper()
	schema, err := compiler.Compile(documentURL + "#/components/schemas/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func validateJSON(t *testing.T, schema *jsonschema.Schema, data []byte, valid bool) {
	t.Helper()
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	err = schema.Validate(value)
	if (err == nil) != valid {
		t.Fatalf("valid=%t, got %v for %s", valid, err, data)
	}
}
