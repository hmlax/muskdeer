package handler

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"google.golang.org/protobuf/proto"

	api "github.com/hmlax/muskdeer/admin/biz/model/admin/v1"
	"github.com/hmlax/muskdeer/admin/internal/transport"
	rpc "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func TestInstanceStatusHTTPQueryPreservesOptionalFalseThroughRPC(t *testing.T) {
	for _, test := range []struct {
		name  string
		query string
		want  *bool
	}{
		{
			name: "all",
		},
		{
			name:  "started",
			query: "?desiredRunning=true",
			want:  proto.Bool(true),
		},
		{
			name:  "stopped",
			query: "?desiredRunning=false",
			want:  proto.Bool(false),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := app.NewContext(0)
			request.Request.SetRequestURI("/admin/v1/muskdeer/instances" + test.query)
			input := &api.MuskdeerInstanceListRequest{}
			if err := transport.BindRequest(request, input, ""); err != nil {
				t.Fatal(err)
			}
			mapped := toRPCMuskdeerInstanceListRequest(input)
			wire, err := proto.Marshal(mapped)
			if err != nil {
				t.Fatal(err)
			}
			var received rpc.MuskdeerInstanceListRequest
			if err := proto.Unmarshal(wire, &received); err != nil {
				t.Fatal(err)
			}
			if (received.DesiredRunning == nil) != (test.want == nil) || test.want != nil && received.GetDesiredRunning() != *test.want {
				t.Fatalf("status query %q lost presence/value: %v", test.query, received.DesiredRunning)
			}
			if input.DesiredRunning != nil {
				*input.DesiredRunning = !*input.DesiredRunning
				if mapped.GetDesiredRunning() != *test.want {
					t.Fatal("API mutation changed RPC filter")
				}
			}
		})
	}
	request := app.NewContext(0)
	request.Request.SetRequestURI("/admin/v1/muskdeer/instances?desiredRunning=unknown")
	if err := transport.BindRequest(request, &api.MuskdeerInstanceListRequest{}, ""); err == nil {
		t.Fatal("invalid status was accepted as all")
	}
}
