package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func sourceIDL(t *testing.T) *descriptorpb.FileDescriptorSet {
	t.Helper()
	command := exec.Command("buf", "build", "--as-file-descriptor-set", "-o", "-")
	command.Dir = "../../../.."
	wire, err := command.Output()
	require.NoError(t, err)
	raw := new(descriptorpb.FileDescriptorSet)
	require.NoError(t, proto.Unmarshal(wire, raw))
	registry, err := protodesc.NewFiles(raw)
	require.NoError(t, err)
	result := new(descriptorpb.FileDescriptorSet)
	require.NoError(t, (proto.UnmarshalOptions{Resolver: dynamicpb.NewTypes(registry)}).Unmarshal(wire, result))
	return result
}

func methodIn(t *testing.T, files *descriptorpb.FileDescriptorSet, pkg, service, name string) *descriptorpb.MethodDescriptorProto {
	t.Helper()
	for _, file := range files.File {
		if file.GetPackage() != pkg {
			continue
		}
		for _, item := range file.Service {
			if item.GetName() != service {
				continue
			}
			for _, method := range item.Method {
				if method.GetName() == name {
					return method
				}
			}
		}
	}
	t.Fatalf("method not found: %s.%s.%s", pkg, service, name)
	return nil
}

func changeRoute(t *testing.T, method *descriptorpb.MethodDescriptorProto, path string) {
	t.Helper()
	var annotation protoreflect.ExtensionType
	proto.RangeExtensions(method.Options, func(ext protoreflect.ExtensionType, _ any) bool {
		if ext.TypeDescriptor().FullName() == "api.get" {
			annotation = ext
		}
		return true
	})
	require.NotNil(t, annotation)
	proto.SetExtension(method.Options, annotation, path)
}

func TestStreamBindingFollowsAdminIDLRouteAndTypes(t *testing.T) {
	files := sourceIDL(t)
	method := methodIn(t, files, "admin.v1", "UserService", "Get")
	method.ServerStreaming = proto.Bool(true)
	methodIn(t, files, "admin.v1", "UserService", "GetByUsername").ServerStreaming = proto.Bool(true)
	methodIn(t, files, "operator.v1", "UserService", "Get").ServerStreaming = proto.Bool(true)
	changeRoute(t, method, "/v2/users/:id")
	_, bindings, err := resolveHTTP(files)
	require.NoError(t, err)
	schema, err := newSchema(files)
	require.NoError(t, err)
	contracts, err := generateContracts(schema, bindings)
	require.NoError(t, err)
	schemaForBindings, err := newSchema(files)
	require.NoError(t, err)
	result, err := generateBindings(schemaForBindings, bindings)
	require.NoError(t, err)
	for _, expected := range []string{`UserServiceGetHTTPPath = "/v2/users/{id}"`, `*GetUserRequest`, `func(*User) error`} {
		require.Contains(t, string(result)+string(contracts["user_service_http.pb.go"]), expected)
	}
}

func TestAdminIDLProjectionPreservesAliasesAndBodyShape(t *testing.T) {
	projection, bindings, err := resolveHTTP(sourceIDL(t))
	require.NoError(t, err)
	method := methodIn(t, projection, "admin.v1", "UserService", "Get")
	rule := proto.GetExtension(method.Options, annotations.E_Http).(*annotations.HttpRule)
	require.Equal(t, "/admin/v1/users/{id}", rule.GetGet())
	require.Equal(t, "/admin/v1/users/username/{username}", rule.AdditionalBindings[0].GetGet())
	method = methodIn(t, projection, "admin.v1", "UserService", "Create")
	rule = proto.GetExtension(method.Options, annotations.E_Http).(*annotations.HttpRule)
	require.Equal(t, "*", rule.GetBody())
	login := methodIn(t, projection, "admin.v1", "AuthenticationService", "Login")
	require.Contains(t, login.GetOptions().String(), "security")
	schemaForBindings, err := newSchema(sourceIDL(t))
	require.NoError(t, err)
	result, err := generateBindings(schemaForBindings, bindings)
	require.NoError(t, err)
	require.Contains(t, string(result), "BindUserServiceGetByUsername")
	require.Contains(t, string(result), "service.Get(ctx, in)")
	for _, file := range projection.File {
		require.NotEqual(t, "operator.v1", file.GetPackage())
	}
}

func TestAPIContractUsesIndependentFieldNumbers(t *testing.T) {
	files := sourceIDL(t)
	for _, file := range files.File {
		if file.GetPackage() != "admin.v1" {
			continue
		}
		for _, message := range file.MessageType {
			if message.GetName() == "GetUserRequest" {
				message.Field[0].Number = proto.Int32(99)
			}
		}
	}
	_, bindings, err := resolveHTTP(files)
	require.NoError(t, err)
	for _, binding := range bindings {
		if binding.service == "UserService" && binding.method == "Get" {
			require.Equal(t, ".admin.v1.GetUserRequest", binding.input)
			require.Equal(t, ".operator.v1.GetUserRequest", binding.rpcInput)
		}
	}
}
func TestAPIRejectsSharedBusinessProtocolDependency(t *testing.T) {
	files := sourceIDL(t)
	for _, file := range files.File {
		if file.GetName() == "admin/v1/user.proto" {
			file.Dependency = append(file.Dependency, "operator/v1/user.proto")
		}
	}
	_, _, err := resolveHTTP(files)
	require.ErrorContains(t, err, "must own its messages")
}

func TestOperatorRejectsAPIDependency(t *testing.T) {
	files := sourceIDL(t)
	for _, file := range files.File {
		if file.GetName() == "operator/v1/user.proto" {
			file.Dependency = append(file.Dependency, "admin/v1/user_types.proto")
		}
	}
	_, _, err := resolveHTTP(files)
	require.ErrorContains(t, err, "RPC must not depend on API")
}

func TestInternalRPCMethodDoesNotRequireAnAPICounterpart(t *testing.T) {
	files := sourceIDL(t)
	for _, file := range files.File {
		if file.GetPackage() != "operator.v1" {
			continue
		}
		for _, service := range file.Service {
			if service.GetName() == "UserService" {
				service.Method = append(service.Method, &descriptorpb.MethodDescriptorProto{
					Name:       proto.String("InternalCount"),
					InputType:  proto.String(".google.protobuf.Empty"),
					OutputType: proto.String(".operator.v1.CountUserResponse"),
				})
			}
		}
	}
	_, bindings, err := resolveHTTP(files)
	require.NoError(t, err)
	for _, binding := range bindings {
		require.NotEqual(t, "InternalCount", binding.method)
	}
	s, err := newSchema(files)
	require.NoError(t, err)
	adapter, err := generateRPCAdapter(s, "UserService", "UserService")
	require.NoError(t, err)
	require.Contains(t, string(adapter), "s.inner.InternalCount(ctx, in)")
	require.NotContains(t, string(adapter), apiModelPath)
}

func TestAliasProjectionPreservesMethodDocumentation(t *testing.T) {
	input := sourceIDL(t)
	projected, _, err := resolveHTTP(input)
	require.NoError(t, err)
	original, err := protodesc.NewFiles(input)
	require.NoError(t, err)
	registry, err := protodesc.NewFiles(projected)
	require.NoError(t, err)
	registry.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "admin.v1" {
			return true
		}
		for i := 0; i < file.Services().Len(); i++ {
			methods := file.Services().Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				before, err := original.FindDescriptorByName(method.FullName())
				require.NoError(t, err)
				require.Equal(t,
					before.ParentFile().SourceLocations().ByDescriptor(before).LeadingComments,
					file.SourceLocations().ByDescriptor(method).LeadingComments,
					string(method.FullName()),
				)
			}
		}
		return true
	})
}
func TestAdminIDLRejectsRPCStreamMismatch(t *testing.T) {
	files := sourceIDL(t)
	methodIn(t, files, "admin.v1", "UserService", "Get").ServerStreaming = proto.Bool(true)
	_, _, err := resolveHTTP(files)
	require.ErrorContains(t, err, "streaming direction")
}

func TestAdminIDLRejectsDuplicateRouteAndUnknownPathField(t *testing.T) {
	for _, path := range []string{"/admin/v1/users", "/admin/v1/users/:unknown_id"} {
		t.Run(path, func(t *testing.T) {
			files := sourceIDL(t)
			changeRoute(t, methodIn(t, files, "admin.v1", "UserService", "Get"), path)
			_, _, err := resolveHTTP(files)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), "duplicate HTTP route") || strings.Contains(err.Error(), "path parameter"), err)
		})
	}
}
