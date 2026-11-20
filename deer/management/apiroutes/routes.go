// Package apiroutes exposes HTTP route metadata to the gRPC business service.
package apiroutes

import (
	"sort"

	"github.com/go-kratos/kratos/v2/transport/http"

	// Register the RPC descriptors used by route discovery.
	_ "github.com/hmlax/muskdeer/deer/pb/operator/v1"
	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Walker reads the same protobuf bindings used by the API, without starting HTTP.
type Walker struct{}

func (Walker) WalkRoute(visit http.WalkRouteFunc) error {
	routes := make([]http.RouteInfo, 0)
	var add func(*annotations.HttpRule)
	add = func(rule *annotations.HttpRule) {
		method, path := "", ""
		switch p := rule.GetPattern().(type) {
		case *annotations.HttpRule_Get:
			method, path = "GET", p.Get
		case *annotations.HttpRule_Post:
			method, path = "POST", p.Post
		case *annotations.HttpRule_Put:
			method, path = "PUT", p.Put
		case *annotations.HttpRule_Delete:
			method, path = "DELETE", p.Delete
		case *annotations.HttpRule_Patch:
			method, path = "PATCH", p.Patch
		case *annotations.HttpRule_Custom:
			method, path = p.Custom.GetKind(), p.Custom.GetPath()
		}
		if path != "" {
			routes = append(routes, http.RouteInfo{
				Path:   path,
				Method: method,
			})
		}
		for _, binding := range rule.GetAdditionalBindings() {
			add(binding)
		}
	}
	protoregistry.GlobalFiles.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		if file.Package() != "operator.v1" {
			return true
		}
		for i := 0; i < file.Services().Len(); i++ {
			methods := file.Services().Get(i).Methods()
			for j := 0; j < methods.Len(); j++ {
				options := methods.Get(j).Options()
				if proto.HasExtension(options, annotations.E_Http) {
					add(proto.GetExtension(options, annotations.E_Http).(*annotations.HttpRule))
				}
			}
		}
		return true
	})
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
	for _, route := range routes {
		if err := visit(route); err != nil {
			return err
		}
	}
	return nil
}
