package adminhttp

import (
	"net"
	"testing"

	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestManagementOriginValidationRejectsImplicitTrustAndRemoteHTTP(t *testing.T) {
	valid := &configv1.ServiceConfig{
		Name:     "api",
		Postgres: &configv1.Postgres{},
		Http:     &configv1.Listener{Address: "127.0.0.1:0"},
		AdminHttp: &configv1.AdminHTTP{
			Enabled:               true,
			PublicOrigin:          "http://127.0.0.1:0",
			AllowInsecureLoopback: true,
		},
	}
	if err := ValidateConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*configv1.ServiceConfig){
		func(v *configv1.ServiceConfig) { v.Name = "strategy" },
		func(v *configv1.ServiceConfig) { v.Postgres = nil },
		func(v *configv1.ServiceConfig) { v.Http.Address = "0.0.0.0:8080" },
		func(v *configv1.ServiceConfig) { v.Http.Address = "127.0.0.1:bad" },
		func(v *configv1.ServiceConfig) { v.AdminHttp.PublicOrigin = "http://outside.example.test:8000" },
		func(v *configv1.ServiceConfig) { v.AdminHttp.PublicOrigin = "http://localhost:8000" },
		func(v *configv1.ServiceConfig) { v.AdminHttp.AllowInsecureLoopback = false },
		func(v *configv1.ServiceConfig) { v.AdminHttp.PublicOrigin += "/path" },
		func(v *configv1.ServiceConfig) { v.AdminHttp.PublicOrigin += "?private=password" },
		func(v *configv1.ServiceConfig) { v.AdminHttp.PublicOrigin = "http://user:password@127.0.0.1:0" },
		func(v *configv1.ServiceConfig) {
			v.AdminHttp.PublicOrigin = "https://admin.example.test:"
			v.AdminHttp.AllowInsecureLoopback = false
		},
		func(v *configv1.ServiceConfig) { v.AdminHttp.Enabled = false },
	} {
		candidate := proto.CloneOf(valid)
		mutate(candidate)
		if err := ValidateConfig(candidate); err == nil || err.Error() != MessageInvalidConfig {
			t.Fatal("unsafe configuration passed or echoed its contents")
		}
	}
	origin, secure, err := resolvedOrigin(valid.AdminHttp, &net.TCPAddr{
		IP:   net.ParseIP("127.0.0.1"),
		Port: 31801,
	})
	if err != nil || secure || origin != "http://127.0.0.1:31801" {
		t.Fatal("dynamic origin did not come from the reserved listener")
	}
	valid.AdminHttp.PublicOrigin, valid.AdminHttp.AllowInsecureLoopback = testOrigin, false
	if err := ValidateConfig(valid); err != nil {
		t.Fatal(err)
	}
}
