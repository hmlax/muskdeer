package bootstrap

import (
	"context"
	"errors"
	"net"
	"testing"

	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	bLogger "github.com/tx7do/kratos-bootstrap/logger"
)

func TestNativeLifecycleClosesSiblingListenerOnStartupFailure(t *testing.T) {
	servers := make([]*HTTPServer, 0, 2)
	for range 2 {
		srv, err := NewHTTPServer("127.0.0.1:0", "", &conf.Bootstrap{})
		if err != nil {
			t.Fatal(err)
		}
		servers = append(servers, srv)
	}
	failure := errors.New("startup hook failed")
	servers[1].hertz.OnRun = append(servers[1].hertz.OnRun, func(context.Context) error { return failure })
	boot := bootstrap.NewContextWithParam(t.Context(), &conf.AppInfo{AppId: "admin"}, &conf.Bootstrap{}, bLogger.NopLogger())
	if err := Run(t.Context(), boot, servers); !errors.Is(err, failure) {
		t.Fatalf("startup error = %v", err)
	}
	for _, srv := range servers {
		listener, err := net.Listen("tcp", srv.listener.Addr().String())
		if err != nil {
			t.Fatalf("listener was leaked: %v", err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
