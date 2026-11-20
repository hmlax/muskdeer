package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type httpBinding struct {
	service   string
	method    string
	rpc       string
	verb      string
	path      string
	body      string
	input     string
	output    string
	rpcInput  string
	rpcOutput string
	stream    bool
}

var hzPathParameter = regexp.MustCompile(`(^|/):([A-Za-z_][A-Za-z0-9_]*)`)

func httpPath(path string) string {
	return hzPathParameter.ReplaceAllString(path, "$1{$2}")
}

func resolveHTTP(input *descriptorpb.FileDescriptorSet) (*descriptorpb.FileDescriptorSet, []httpBinding, error) {
	registry, err := protodesc.NewFiles(input)
	if err != nil {
		return nil, nil, err
	}
	// Resolve source annotations from this descriptor set. Generated Go packages
	// can still contain an earlier schema during regeneration.
	resolver := dynamicpb.NewTypes(registry)
	wire, err := proto.Marshal(input)
	if err != nil {
		return nil, nil, err
	}
	files := new(descriptorpb.FileDescriptorSet)
	if err := (proto.UnmarshalOptions{Resolver: resolver}).Unmarshal(wire, files); err != nil {
		return nil, nil, err
	}
	option := func(method *descriptorpb.MethodDescriptorProto, name string) (string, error) {
		_, err := resolver.FindExtensionByName(protoreflect.FullName(name))
		if err != nil {
			return "", fmt.Errorf("missing IDL annotation %s: %w", name, err)
		}
		var result string
		proto.RangeExtensions(method.GetOptions(), func(ext protoreflect.ExtensionType, value any) bool {
			if string(ext.TypeDescriptor().FullName()) == name {
				var ok bool
				result, ok = value.(string)
				if !ok {
					err = fmt.Errorf("IDL annotation %s must be a string", name)
				}
				return false
			}
			return true
		})
		return result, err
	}
	methods := make(map[string]*descriptorpb.MethodDescriptorProto)
	for _, file := range files.File {
		if file.GetPackage() != "operator.v1" {
			continue
		}
		for _, dependency := range file.Dependency {
			if strings.HasPrefix(dependency, "admin/") || strings.HasPrefix(dependency, "hertz/") {
				return nil, nil, fmt.Errorf("operator RPC must not depend on API protocol %s", dependency)
			}
		}
		for _, service := range file.Service {
			for _, method := range service.Method {
				if proto.HasExtension(method.Options, annotations.E_Http) {
					return nil, nil, fmt.Errorf("HTTP route belongs in admin/idl/proto, not %s", file.GetName())
				}
				key := service.GetName() + "." + method.GetName()
				methods[key] = method
			}
		}
	}
	var bindings []httpBinding
	state := httpResolver{
		registry: registry,
		option:   option,
		methods:  methods,
		seen:     make(map[string]bool),
	}
	for _, file := range files.File {
		if file.GetPackage() != "admin.v1" {
			continue
		}
		for _, dependency := range file.Dependency {
			if strings.HasPrefix(dependency, "operator/") || strings.HasPrefix(dependency, "pagination/") {
				return nil, nil, fmt.Errorf("admin API must own its messages: forbidden dependency %s", dependency)
			}
		}
		for _, service := range file.Service {
			for _, method := range service.Method {
				resolved, err := state.bind(file, service, method)
				if err != nil {
					return nil, nil, err
				}
				bindings = append(bindings, resolved...)
			}
		}
	}
	if len(bindings) == 0 {
		return nil, nil, fmt.Errorf("admin/idl/proto contains no HTTP binding")
	}
	projection, err := state.projectAPI(files)
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(bindings, func(i, j int) bool {
		left, right := bindings[i], bindings[j]
		return left.service+"."+left.method+left.verb < right.service+"."+right.method+right.verb
	})
	return projection, bindings, nil
}

// Asset plugins resolve documentation by descriptor indexes. Folding an alias
// must move its remaining siblings' locations along with their descriptors.
func remapMethodLocations(file *descriptorpb.FileDescriptorProto, service int32, indexes map[int32]int32) {
	if file.SourceCodeInfo == nil {
		return
	}
	var kept []*descriptorpb.SourceCodeInfo_Location
	for _, location := range file.SourceCodeInfo.Location {
		path := location.Path
		if len(path) >= 4 && path[0] == 6 && path[1] == service && path[2] == 2 {
			index, ok := indexes[path[3]]
			if !ok {
				continue
			}
			path[3] = index
		}
		kept = append(kept, location)
	}
	file.SourceCodeInfo.Location = kept
}

func addDependency(file *descriptorpb.FileDescriptorProto, path string) {
	for _, existing := range file.Dependency {
		if existing == path {
			return
		}
	}
	file.Dependency = append(file.Dependency, path)
}

func ensureDescriptor(files *descriptorpb.FileDescriptorSet, descriptor protoreflect.FileDescriptor) {
	for _, file := range files.File {
		if file.GetName() == descriptor.Path() {
			return
		}
	}
	for i := 0; i < descriptor.Imports().Len(); i++ {
		ensureDescriptor(files, descriptor.Imports().Get(i).FileDescriptor)
	}
	files.File = append(files.File, protodesc.ToFileDescriptorProto(descriptor))
}

func validateBinding(registry *protoregistry.Files, binding httpBinding) error {
	if binding.stream && binding.verb != "GET" {
		return fmt.Errorf("SSE method %s.%s must use GET", binding.service, binding.method)
	}
	descriptor, err := registry.FindDescriptorByName(protoreflect.FullName(strings.TrimPrefix(binding.input, ".")))
	if err != nil {
		return err
	}
	message := descriptor.(protoreflect.MessageDescriptor)
	for _, match := range hzPathParameter.FindAllStringSubmatch(binding.path, -1) {
		if message.Fields().ByName(protoreflect.Name(match[2])) == nil && message.Fields().ByJSONName(match[2]) == nil {
			return fmt.Errorf("path parameter %q missing from %s", match[2], binding.input)
		}
	}
	if binding.body != "" && binding.body != "*" && message.Fields().ByName(protoreflect.Name(binding.body)) == nil {
		return fmt.Errorf("body field %q missing from %s", binding.body, binding.input)
	}
	return nil
}

type httpResolver struct {
	registry *protoregistry.Files
	option   func(*descriptorpb.MethodDescriptorProto, string) (string, error)
	methods  map[string]*descriptorpb.MethodDescriptorProto
	seen     map[string]bool
}

func (r *httpResolver) bind(file *descriptorpb.FileDescriptorProto, service *descriptorpb.ServiceDescriptorProto, method *descriptorpb.MethodDescriptorProto) ([]httpBinding, error) {
	var bindings []httpBinding
	if !apiMessage(method.GetInputType()) || !apiMessage(method.GetOutputType()) {
		return nil, fmt.Errorf("API %s.%s must own its request and response messages", service.GetName(), method.GetName())
	}
	rpc, err := r.option(method, "admin.http.rpc")
	if err != nil {
		return nil, err
	}
	if rpc == "" {
		rpc = method.GetName()
	}
	key := service.GetName() + "." + rpc
	target := r.methods[key]
	if target == nil {
		return nil, fmt.Errorf("admin method %s.%s has no operator RPC %s", service.GetName(), method.GetName(), key)
	}
	if target.GetServerStreaming() != method.GetServerStreaming() || target.GetClientStreaming() != method.GetClientStreaming() {
		return nil, fmt.Errorf("admin method %s.%s must match streaming direction of operator RPC %s", service.GetName(), method.GetName(), key)
	}
	body, err := r.option(method, "admin.http.body")
	if err != nil {
		return nil, err
	}
	count := 0
	for _, verb := range []string{"get", "post", "put", "delete", "patch", "head", "options"} {
		path, err := r.option(method, "api."+verb)
		if err != nil {
			return nil, err
		}
		if path == "" {
			continue
		}
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "{}*") {
			return nil, fmt.Errorf("invalid hz path %q: use /resource/:id templates", path)
		}
		route := strings.ToUpper(verb) + " " + path
		if r.seen[route] {
			return nil, fmt.Errorf("duplicate HTTP route %s", route)
		}
		r.seen[route] = true
		count++
		binding := httpBinding{
			service:   service.GetName(),
			method:    method.GetName(),
			rpc:       rpc,
			verb:      strings.ToUpper(verb),
			path:      path,
			body:      body,
			input:     method.GetInputType(),
			output:    method.GetOutputType(),
			rpcInput:  target.GetInputType(),
			rpcOutput: target.GetOutputType(),
			stream:    method.GetServerStreaming(),
		}
		if err := validateBinding(r.registry, binding); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
		rule := &annotations.HttpRule{Body: body}
		patternPath := httpPath(path)
		switch verb {
		case "get":
			rule.Pattern = &annotations.HttpRule_Get{Get: patternPath}
		case "post":
			rule.Pattern = &annotations.HttpRule_Post{Post: patternPath}
		case "put":
			rule.Pattern = &annotations.HttpRule_Put{Put: patternPath}
		case "delete":
			rule.Pattern = &annotations.HttpRule_Delete{Delete: patternPath}
		case "patch":
			rule.Pattern = &annotations.HttpRule_Patch{Patch: patternPath}
		default:
			rule.Pattern = &annotations.HttpRule_Custom{
				Custom: &annotations.CustomHttpPattern{
					Kind: strings.ToUpper(verb),
					Path: patternPath,
				},
			}
		}
		if method.Options == nil {
			method.Options = new(descriptorpb.MethodOptions)
		}
		proto.SetExtension(method.Options, annotations.E_Http, rule)
		addDependency(file, "google/api/annotations.proto")
	}
	if count == 0 {
		return nil, fmt.Errorf("admin method %s.%s has no hz HTTP annotation: %v", service.GetName(), method.GetName(), method.GetOptions())
	}
	return bindings, nil
}

func (r *httpResolver) projectAPI(files *descriptorpb.FileDescriptorSet) (*descriptorpb.FileDescriptorSet, error) {
	// API assets derive only from admin.v1. Operator descriptors are never
	// rewritten into public HTTP services or exposed to frontend generators.
	projection := new(descriptorpb.FileDescriptorSet)
	for _, file := range files.File {
		if file.GetPackage() == "operator.v1" {
			continue
		}
		if file.GetPackage() == "admin.v1" {
			for serviceIndex, service := range file.Service {
				var kept []*descriptorpb.MethodDescriptorProto
				methodIndexes := make(map[int32]int32)
				for methodIndex, method := range service.Method {
					rpc, err := r.option(method, "admin.http.rpc")
					if err != nil {
						return nil, err
					}
					var base *descriptorpb.MethodDescriptorProto
					for _, candidate := range service.Method {
						if candidate.GetName() == rpc {
							base = candidate
							break
						}
					}
					if base != nil && rpc != method.GetName() && base.GetInputType() == method.GetInputType() && base.GetOutputType() == method.GetOutputType() {
						rule := proto.GetExtension(method.Options, annotations.E_Http).(*annotations.HttpRule)
						parent := proto.GetExtension(base.Options, annotations.E_Http).(*annotations.HttpRule)
						parent.AdditionalBindings = append(parent.AdditionalBindings, rule)
					} else {
						methodIndexes[int32(methodIndex)] = int32(len(kept))
						kept = append(kept, method)
					}
				}
				service.Method = kept
				remapMethodLocations(file, int32(serviceIndex), methodIndexes)
			}
		}
		projection.File = append(projection.File, file)
	}
	ensureDescriptor(projection, annotations.File_google_api_annotations_proto)
	return projection, nil
}
