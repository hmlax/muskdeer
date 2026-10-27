package deer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
)

func TestHTTPRequestIDsReachResponseContextAndRootLog(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	cfg := serviceConfig("request-correlation")
	cfg.Grpc = nil
	cfg.Http = &configv1.Listener{
		Address:        "127.0.0.1:0",
		RequestTimeout: durationpb.New(time.Second),
	}
	api, cleanup, err := NewDeerApi(t.Context(), cfg, APISpec{
		Service: ServiceSpec{LogOutput: &output},
		Routes: func(_ context.Context, owner *Deer, hertz *server.Hertz) error {
			hertz.GET("/request-id", func(ctx context.Context, response *app.RequestContext) {
				owner.Logger().Slog().WithGroup("operation").InfoContext(ctx, "correlation.http")
				response.String(http.StatusOK, requestid.FromContext(ctx).String())
			})
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	runDeer(t, api.Deer)
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	var observed []string
	for _, values := range [][]string{{"edge-a_1.2-3"}, nil, {"bad value"}, {"first", "second"}} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+api.HTTPAddress()+"/request-id", nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			request.Header.Add(requestid.Header, value)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read request correlation: %v, %v", err, closeErr)
		}
		id := response.Header.Get(requestid.Header)
		if string(body) != id || id == "" {
			t.Fatalf("response/context IDs differ: %s, %s", body, id)
		}
		if len(values) == 1 && values[0] == "edge-a_1.2-3" {
			if id != values[0] {
				t.Fatal("valid edge ID was replaced")
			}
		} else if len(id) != 32 || strings.Contains(id, "first") || id == "bad value" {
			t.Fatal("missing/invalid/duplicate ID was not normalized")
		}
		observed = append(observed, id)
	}
	if err := cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var logged []string
	for decoder.More() {
		var record struct {
			Message   string `json:"msg"`
			RequestID string `json:"request_id"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Message == "correlation.http" {
			logged = append(logged, record.RequestID)
		}
	}
	if len(logged) != len(observed) {
		t.Fatalf("correlated log count = %d, want %d", len(logged), len(observed))
	}
	for index, id := range observed {
		if logged[index] != id {
			t.Fatal("HTTP request and scoped root log correlation differ")
		}
	}
}
