package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type conversion struct {
	source, target, prefix, name string
	enum                         bool
}
type converter struct {
	schema     *schema
	file       *protogen.GeneratedFile
	queue      []conversion
	registered map[string]string
	names      map[string]string
}

func (c *converter) register(source, target, prefix string, enum bool) (string, error) {
	key := prefix + source + "->" + target
	if name := c.registered[key]; name != "" {
		return name, nil
	}
	var ident protogen.GoIdent
	if enum {
		a, b := c.schema.enums[source], c.schema.enums[target]
		if a == nil || b == nil {
			return "", fmt.Errorf("unmapped enum %s -> %s", source, target)
		}
		ident = a.GoIdent
		if prefix == "toAPI" {
			ident = b.GoIdent
		}
	} else {
		a, b := c.schema.messages[source], c.schema.messages[target]
		if a == nil || b == nil {
			return "", fmt.Errorf("unmapped message %s -> %s", source, target)
		}
		ident = a.GoIdent
		if prefix == "toAPI" {
			ident = b.GoIdent
		}
	}
	name := prefix + ident.GoName
	if existing := c.names[name]; existing != "" && existing != key {
		return "", fmt.Errorf("ambiguous explicit mapping %s: %s and %s", name, existing, key)
	}
	c.names[name] = key
	c.registered[key] = name
	c.queue = append(c.queue, conversion{
		source: source,
		target: target,
		prefix: prefix,
		name:   name,
		enum:   enum,
	})
	return name, nil
}
func (c *converter) valueType(field *protogen.Field) string {
	if field.Message != nil {
		return "*" + c.file.QualifiedGoIdent(field.Message.GoIdent)
	}
	if field.Enum != nil {
		return c.file.QualifiedGoIdent(field.Enum.GoIdent)
	}
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return "bool"
	case protoreflect.StringKind:
		return "string"
	case protoreflect.BytesKind:
		return "[]byte"
	case protoreflect.DoubleKind:
		return "float64"
	case protoreflect.FloatKind:
		return "float32"
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	}
	panic("unsupported protobuf field kind")
}
func realOneof(field *protogen.Field) bool {
	return field.Oneof != nil && !field.Oneof.Desc.IsSynthetic()
}
func (c *converter) value(dst, src string, a, b *protogen.Field, prefix string, inside bool) error {
	if a.Desc.Kind() != b.Desc.Kind() {
		return fmt.Errorf("field %s -> %s changes kind; provide an explicit adapter", a.Desc.FullName(), b.Desc.FullName())
	}
	if !inside && a.Desc.IsMap() {
		if !b.Desc.IsMap() {
			return fmt.Errorf("map cardinality mismatch")
		}
		ak, av := a.Message.Fields[0], a.Message.Fields[1]
		bk, bv := b.Message.Fields[0], b.Message.Fields[1]
		if ak.Desc.Kind() != bk.Desc.Kind() {
			return fmt.Errorf("map key kind mismatch")
		}
		c.file.P("if ", src, "!=nil{", dst, "=make(map[", c.valueType(bk), "]", c.valueType(bv), ",len(", src, "));for key,value:=range ", src, "{")
		c.file.P("var mapped ", c.valueType(bv))
		if err := c.value("mapped", "value", av, bv, prefix, true); err != nil {
			return err
		}
		c.file.P(dst, "[key]=mapped }}")
		return nil
	}
	if !inside && a.Desc.IsList() {
		if !b.Desc.IsList() {
			return fmt.Errorf("list cardinality mismatch")
		}
		c.file.P("if ", src, "!=nil{", dst, "=make([]", c.valueType(b), ",len(", src, "));for index,value:=range ", src, "{")
		if err := c.value(dst+"[index]", "value", a, b, prefix, true); err != nil {
			return err
		}
		c.file.P("}}")
		return nil
	}
	if !inside && (b.Desc.IsList() || b.Desc.IsMap()) {
		return fmt.Errorf("field cardinality mismatch")
	}
	if a.Message != nil {
		name, err := c.register(fullName(a.Message.Desc), fullName(b.Message.Desc), prefix, false)
		if err != nil {
			return err
		}
		c.file.P(dst, "=", name, "(", src, ")")
		return nil
	}
	presence := !inside && a.Desc.HasPresence() && a.Desc.Kind() != protoreflect.BytesKind
	targetPresence := !inside && b.Desc.HasPresence() && b.Desc.Kind() != protoreflect.BytesKind
	if presence {
		c.file.P("if ", src, "!=nil{")
		src = "*" + src
	}
	value := src
	if a.Enum != nil {
		name, err := c.register(fullName(a.Enum.Desc), fullName(b.Enum.Desc), prefix, true)
		if err != nil {
			return err
		}
		value = name + "(" + src + ")"
	} else if a.Desc.Kind() == protoreflect.BytesKind {
		value = qualified(c.file, "slices", "Clone") + "(" + src + ")"
	}
	if targetPresence {
		c.file.P("value:=", value, ";", dst, "=&value")
	} else {
		c.file.P(dst, "=", value)
	}
	if presence {
		c.file.P("}")
	}
	return nil
}
func findField(message *protogen.Message, name protoreflect.Name) *protogen.Field {
	for _, f := range message.Fields {
		if f.Desc.Name() == name {
			return f
		}
	}
	return nil
}
func (c *converter) emitEnum(pair conversion) error {
	a, b := c.schema.enums[pair.source], c.schema.enums[pair.target]
	c.file.P("func ", pair.name, "(in ", c.file.QualifiedGoIdent(a.GoIdent), ") ", c.file.QualifiedGoIdent(b.GoIdent), " {switch in{")
	seen := make(map[protoreflect.EnumNumber]bool)
	for _, value := range a.Values {
		if seen[value.Desc.Number()] {
			continue
		}
		seen[value.Desc.Number()] = true
		var target *protogen.EnumValue
		for _, candidate := range b.Values {
			if candidate.Desc.Name() == value.Desc.Name() {
				target = candidate
				break
			}
		}
		if target == nil {
			return fmt.Errorf("enum %s has no explicit target for %s", pair.target, value.Desc.Name())
		}
		c.file.P("case ", c.file.QualifiedGoIdent(value.GoIdent), ":return ", c.file.QualifiedGoIdent(target.GoIdent))
	}
	c.file.P("default:return ", c.file.QualifiedGoIdent(b.GoIdent), "(in)}}")
	return nil
}
func (c *converter) emitMessage(pair conversion) error {
	a, b := c.schema.messages[pair.source], c.schema.messages[pair.target]
	at, bt := c.file.QualifiedGoIdent(a.GoIdent), c.file.QualifiedGoIdent(b.GoIdent)
	c.file.P("func ", pair.name, "(in *", at, ") *", bt, "{if in==nil{return nil};out:=new(", bt, ")")
	fields := a.Fields
	if pair.prefix == "toAPI" {
		fields = b.Fields
	}
	oneofs := make(map[string]bool)
	for _, field := range fields {
		source, target := field, findField(b, field.Desc.Name())
		if pair.prefix == "toAPI" {
			source = findField(a, field.Desc.Name())
			target = field
		}
		if source == nil || target == nil {
			return fmt.Errorf("%s -> %s: field %s requires an explicit mapping", pair.source, pair.target, field.Desc.Name())
		}
		if realOneof(source) || realOneof(target) {
			if !realOneof(source) || !realOneof(target) {
				return fmt.Errorf("oneof ownership mismatch for %s", field.Desc.FullName())
			}
			group := source.Oneof.GoName + "->" + target.Oneof.GoName
			if oneofs[group] {
				continue
			}
			oneofs[group] = true
			c.file.P("switch value:=in.", source.Oneof.GoName, ".(type){")
			for _, variant := range fields {
				sf, tf := variant, findField(b, variant.Desc.Name())
				if pair.prefix == "toAPI" {
					sf = findField(a, variant.Desc.Name())
					tf = variant
				}
				if sf == nil || tf == nil || !realOneof(sf) || !realOneof(tf) || sf.Oneof != source.Oneof || tf.Oneof != target.Oneof {
					continue
				}
				c.file.P("case *", c.file.QualifiedGoIdent(sf.GoIdent), ":if value==nil{break};mapped:=new(", c.file.QualifiedGoIdent(tf.GoIdent), ")")
				if err := c.value("mapped."+tf.GoName, "value."+sf.GoName, sf, tf, pair.prefix, true); err != nil {
					return err
				}
				c.file.P("out.", tf.Oneof.GoName, "=mapped")
			}
			c.file.P("}")
			continue
		}
		if err := c.value("out."+target.GoName, "in."+source.GoName, source, target, pair.prefix, false); err != nil {
			return err
		}
	}
	c.file.P("return out}")
	return nil
}

type adapterMetadata struct {
	Regs map[string]string `json:"regs"`
	Vars map[string]string `json:"vars"`
}

func generateAdapters(s *schema, bindings []httpBinding) error {
	if err := cleanGeneratedAdapters(); err != nil {
		return err
	}

	data, err := os.ReadFile("scripts/admin/tools/gateway-services.json")
	if err != nil {
		return err
	}
	var metadata adapterMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return err
	}
	file := s.plugin.NewGeneratedFile("converters_generated.go", protogen.GoImportPath(handlerPath))
	generated(file, "handler")
	c := &converter{
		schema:     s,
		file:       file,
		registered: make(map[string]string),
		names:      make(map[string]string),
	}
	groups := services(bindings)
	for _, name := range serviceNames(groups) {
		for _, b := range groups[name] {
			if !apiMessage(b.input) || !apiMessage(b.output) {
				return fmt.Errorf("API %s.%s must own its business messages", name, b.method)
			}
			if _, err := c.register(b.input, b.rpcInput, "toRPC", false); err != nil {
				return err
			}
			if _, err := c.register(b.rpcOutput, b.output, "toAPI", false); err != nil {
				return err
			}
		}
		_, normal := metadata.Regs[name]
		if !normal {
			continue
		}
		proxy := s.plugin.NewGeneratedFile("proxy_"+name+".go", protogen.GoImportPath(handlerPath))
		generated(proxy, "handler")
		context := qualified(proxy, "context", "Context")
		proxy.P("type ", name, "Proxy struct {client ", qualified(proxy, operatorPath, name+"Client"), "}")
		for _, b := range groups[name] {
			if b.stream {
				return fmt.Errorf("streaming service %s needs a dedicated handler", name)
			}
			input, err := s.message(proxy, b.input)
			if err != nil {
				return err
			}
			output, err := s.message(proxy, b.output)
			if err != nil {
				return err
			}
			in, _ := c.register(b.input, b.rpcInput, "toRPC", false)
			out, _ := c.register(b.rpcOutput, b.output, "toAPI", false)
			proxy.P("func(s *", name, "Proxy)", b.method, "(ctx ", context, ",in *", input, ")(*", output, ",error){")
			proxy.P("request:=", in, "(in)")
			proxy.P("if err:=", qualified(proxy, transportPath, "ValidateRequest"), "(request);err!=nil{return nil,err}")
			proxy.P("reply,err:=s.client.", b.rpc, "(ctx,request);if err!=nil{return nil,err}")
			if name == "UserService" {
				proxy.P(qualified(proxy, "github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact/redact/v1", "Apply"), "(reply)")
			}
			proxy.P("return ", out, "(reply),nil}")
		}
		content, err := proxy.Content()
		if err != nil {
			return err
		}
		if err := writeGenerated("admin/biz/handler/proxy_"+snake(name)+"_generated.go", content); err != nil {
			return err
		}
	}
	// RPC registration follows the operator IDL even when a service or method has
	// no public HTTP endpoint. Adding an internal RPC never creates an API DTO.
	rpcNames := make([]string, 0, len(metadata.Regs))
	for name := range metadata.Regs {
		rpcNames = append(rpcNames, name)
	}
	sort.Strings(rpcNames)
	for _, name := range rpcNames {
		service := s.rpcServices[name]
		if service == nil {
			return fmt.Errorf("registered RPC service %s is missing", name)
		}
		variable := metadata.Regs[name]
		content, err := generateRPCAdapter(s, name, metadata.Vars[variable])
		if err != nil {
			return err
		}
		if err := writeGenerated("operator/internal/server/adapter_"+snake(name)+"_generated.go", content); err != nil {
			return err
		}

	}

	// Sorting the roots above makes conversion expansion deterministic. Newly found
	// nested fields are queued and emitted as typed assignments, never serialized.
	for index := 0; index < len(c.queue); index++ {
		pair := c.queue[index]
		if pair.enum {
			err = c.emitEnum(pair)
		} else {
			err = c.emitMessage(pair)
		}
		if err != nil {
			return err
		}
	}
	content, err := file.Content()
	if err != nil {
		return err
	}
	return writeGenerated("admin/biz/handler/converters_generated.go", content)
}

func generateRPCAdapter(s *schema, name, implementation string) ([]byte, error) {
	service := s.rpcServices[name]
	if service == nil {
		return nil, fmt.Errorf("registered RPC service %s is missing", name)
	}
	adapter := s.plugin.NewGeneratedFile("adapter_"+name+".go", protogen.GoImportPath("github.com/hmlax/muskdeer/operator/internal/server"))
	generated(adapter, "server")
	adapter.P("type ", name, "Adapter struct {", qualified(adapter, operatorPath, "Unimplemented"+name+"Server"), ";inner *", qualified(adapter, "github.com/hmlax/muskdeer/operator/internal/service", implementation), "}")
	for _, method := range service.Methods {
		if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
			return nil, fmt.Errorf("streaming RPC %s requires its own service adapter", method.Desc.FullName())
		}
		call := method.GoName
		if name == "FileTransferService" && (call == "PutUploadFile" || call == "PostUploadFile") {
			call = "UploadFile"
		}
		adapter.P("func(s *", name, "Adapter)", method.GoName, "(ctx ", qualified(adapter, "context", "Context"), ",in *", adapter.QualifiedGoIdent(method.Input.GoIdent), ")(*", adapter.QualifiedGoIdent(method.Output.GoIdent), ",error){return s.inner.", call, "(ctx,in)}")
	}
	return adapter.Content()
}

func cleanGeneratedAdapters() error {
	for _, pattern := range []string{
		"admin/biz/handler/proxy_*_generated.go",
		"operator/internal/server/adapter_*_generated.go",
	} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}
		for _, path := range paths {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.HasPrefix(content, []byte("// Code generated by scripts/admin/tools/httpgen")) {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
