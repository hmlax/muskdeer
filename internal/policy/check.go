// Package policy checks owned source and Proto conventions before generation.
package policy

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"strings"

	"github.com/srikrsna/protoc-gen-gotag/tagger"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Issue identifies a source convention violation without printing input data.
type Issue struct {
	File    string
	Line    int
	Message string
}

// GoSource checks syntax-level float literals and direct Table calls, including
// parenthesized selectors. Semantic identifier/alias checks also run in lint.
func GoSource(fileSet *token.FileSet, file *ast.File) []Issue {
	if ast.IsGenerated(file) {
		return nil
	}
	var issues []Issue
	report := func(position token.Pos, message string) {
		location := fileSet.Position(position)
		issues = append(issues, Issue{
			File:    location.Filename,
			Line:    location.Line,
			Message: message,
		})
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.BasicLit:
			if value.Kind == token.FLOAT {
				report(value.Pos(), "floating-point literals are forbidden; use decimal strings")
			}
		case *ast.CallExpr:
			callee := value.Fun
			for {
				parenthesized, ok := callee.(*ast.ParenExpr)
				if !ok {
					break
				}
				callee = parenthesized.X
			}
			if selector, ok := callee.(*ast.SelectorExpr); ok && selector.Sel.Name == "Table" {
				report(selector.Pos(), "Table calls are forbidden; bind a generated Model")
			}
		case *ast.FuncDecl:
			if value.Recv != nil && value.Name.Name == "TableName" {
				report(value.Pos(), "handwritten TableName overrides are forbidden")
			}
		}
		return true
	})
	return issues
}

// Descriptors rejects float wire fields and verifies every owned persistence
// message's four common fields, generated GORM tags and numeric string types.
func Descriptors(set *descriptorpb.FileDescriptorSet) []Issue {
	var issues []Issue
	for _, file := range set.GetFile() {
		name := file.GetName()
		if !strings.HasPrefix(name, "deer/proto/") && !strings.HasPrefix(name, "api/") {
			continue
		}
		for _, message := range file.GetMessageType() {
			checkMessage(name, message.GetName(), message, strings.HasPrefix(name, "deer/proto/model/"), &issues)
		}
	}
	return issues
}

func checkMessage(file, name string, message *descriptorpb.DescriptorProto, persistence bool, issues *[]Issue) {
	report := func(field, reason string) {
		*issues = append(*issues, Issue{
			File:    file,
			Message: name + "." + field + ": " + reason,
		})
	}
	fields := make(map[string]*descriptorpb.FieldDescriptorProto, len(message.GetField()))
	for _, field := range message.GetField() {
		fields[field.GetName()] = field
		if field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_FLOAT || field.GetType() == descriptorpb.FieldDescriptorProto_TYPE_DOUBLE {
			report(field.GetName(), "decimal wire fields must be string")
		}
		if persistence {
			tag := gormTag(field)
			kind := strings.ToLower(tag["type"])
			if (strings.HasPrefix(kind, "numeric") || strings.HasPrefix(kind, "decimal")) && field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_STRING {
				report(field.GetName(), "numeric persistence fields must use string/decimal boundaries")
			}
		}
	}
	if persistence && !message.GetOptions().GetMapEntry() {
		checkCommonFields(fields, report)
	}
	for _, nested := range message.GetNestedType() {
		checkMessage(file, name+"."+nested.GetName(), nested, persistence, issues)
	}
}

func checkCommonFields(fields map[string]*descriptorpb.FieldDescriptorProto, report func(string, string)) {
	for _, name := range []string{"id", "create_at", "update_at", "delete_at"} {
		field := fields[name]
		if field == nil || field.GetType() != descriptorpb.FieldDescriptorProto_TYPE_INT64 || field.GetLabel() == descriptorpb.FieldDescriptorProto_LABEL_REPEATED {
			report(name, "required scalar int64 common field is missing or invalid")
			continue
		}
		tag := gormTag(field)
		if tag["column"] != name || strings.ToLower(tag["type"]) != "bigint" {
			report(name, "common fields require explicit column and bigint GORM tags")
		}
		switch name {
		case "id":
			if !enabled(tag, "primarykey") || !enabled(tag, "autoincrement") || field.GetProto3Optional() {
				report(name, "Id requires generated primaryKey and autoIncrement")
			}
		case "create_at", "update_at":
			key := "autocreatetime"
			if name == "update_at" {
				key = "autoupdatetime"
			}
			if !enabled(tag, "not null") || tag[key] != "milli" || field.GetProto3Optional() {
				report(name, "time requires NOT NULL and automatic Unix milliseconds")
			}
		case "delete_at":
			if !field.GetProto3Optional() || enabled(tag, "not null") {
				report(name, "deleteAt requires an optional nullable field")
			}
		}
	}
}

func gormTag(field *descriptorpb.FieldDescriptorProto) map[string]string {
	values := make(map[string]string)
	options := field.GetOptions()
	if !proto.HasExtension(options, tagger.E_Tags) {
		return values
	}
	tags, ok := proto.GetExtension(options, tagger.E_Tags).(string)
	if !ok {
		return values
	}
	for _, item := range strings.Split(reflect.StructTag(tags).Get("gorm"), ";") {
		key, value, _ := strings.Cut(item, ":")
		values[strings.ToLower(strings.TrimSpace(key))] = value
	}
	return values
}

func enabled(tags map[string]string, key string) bool {
	value, exists := tags[key]
	return exists && !strings.EqualFold(value, "false")
}

// Format renders a concise location without source snippets or field values.
func Format(issue Issue) string {
	if issue.Line != 0 {
		return fmt.Sprintf("%s:%d: %s", issue.File, issue.Line, issue.Message)
	}
	return issue.File + ": " + issue.Message
}
