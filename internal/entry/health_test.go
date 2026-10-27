package entry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
)

func TestReadinessProbeRequiresReadyBodyAndDoesNotFollowRedirects(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   int
		body   string
		wantOK bool
	}{
		{
			name:   "ready",
			code:   http.StatusOK,
			body:   `{"status":"ready"}`,
			wantOK: true,
		},
		{
			name: "unready",
			code: http.StatusServiceUnavailable,
			body: `{"status":"unready"}`,
		},
		{
			name: "wrong status body",
			code: http.StatusOK,
			body: `{"status":"alive"}`,
		},
		{
			name: "invalid JSON",
			code: http.StatusOK,
			body: "ready",
		},
		{
			name: "oversized body",
			code: http.StatusOK,
			body: `{"status":"ready"}` + strings.Repeat(" ", 256),
		},
		{
			name: "redirect",
			code: http.StatusFound,
			body: `{"status":"ready"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var unexpected atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/readyz" {
					unexpected.Add(1)
				}
				response.Header().Set("Location", "/unexpected")
				response.WriteHeader(test.code)
				if _, err := fmt.Fprint(response, test.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			cfg := &configv1.Listener{
				Address:        strings.TrimPrefix(server.URL, "http://"),
				RequestTimeout: durationpb.New(time.Second),
			}
			if err := CheckReady(t.Context(), cfg); (err == nil) != test.wantOK {
				t.Fatalf("readiness = %v, wantOK=%v", err, test.wantOK)
			}
			if unexpected.Load() != 0 {
				t.Fatal("probe followed a redirect")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := CheckReady(ctx, cfg); !errors.Is(err, context.Canceled) {
				t.Fatalf("probe did not preserve cancellation: %v", err)
			}
		})
	}
}
