package main

import (
	"net/http"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/ut"
	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"

	"github.com/hmlax/muskdeer/admin/biz/handler"
	"github.com/hmlax/muskdeer/admin/internal/transport"
)

func TestRootRegistrationPreservesIDLBindingsAndSwaggerConfig(t *testing.T) {
	for _, swagger := range []bool{false, true} {
		h, err := transport.NewAPIRouter(&conf.Bootstrap{})
		if err != nil {
			t.Fatal(err)
		}
		services := handler.NewServices(nil, nil)
		services.EnableSwagger = swagger
		register(h, services)

		// Invalid typed input must reach the generated binding and fail before
		// any gRPC invocation. A missing route would incorrectly return 404.
		request := ut.CreateUtRequestContext(http.MethodGet, "/admin/v1/users/not-an-id", nil)
		h.ServeHTTP(t.Context(), request)
		if request.Response.StatusCode() != http.StatusBadRequest {
			t.Fatalf("IDL binding status = %d", request.Response.StatusCode())
		}

		request = ut.CreateUtRequestContext(http.MethodGet, "/docs/openapi.yaml", nil)
		h.ServeHTTP(t.Context(), request)
		expected := http.StatusNotFound
		if swagger {
			expected = http.StatusOK
		}
		if request.Response.StatusCode() != expected {
			t.Fatalf("swagger enabled=%t: status=%d", swagger, request.Response.StatusCode())
		}
	}
}
