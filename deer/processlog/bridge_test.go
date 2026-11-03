package processlog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	kratoslog "github.com/go-kratos/kratos/v2/log"

	"github.com/hmlax/muskdeer/deer/logging"
	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/deer/requestid"
)

func TestFrameworkAdaptersKeepIdentityWithoutLoggingArgumentValues(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	target, err := logging.New(t.Context(), &configv1.Logging{
		Level:  logging.LevelDebug,
		Stdout: true,
	}, logging.Identity{
		Service:  "api",
		Instance: "framework-test",
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := target.Close(); err != nil {
			t.Error(err)
		}
	})
	shared := &bridge{target: target}
	hertz := &hertzLogger{bridge: shared}
	id, err := requestid.New(t.Context(), "framework-request")
	if err != nil {
		t.Fatal(err)
	}
	const secret = "framework_argument_must_not_escape"
	hertz.CtxErrorf(requestid.Bind(t.Context(), id), secret+" %s", secret)
	if err := (&kratosLogger{bridge: shared}).Log(kratoslog.LevelWarn, "msg", secret); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) {
		t.Fatal("framework formatting exposed unstructured argument values")
	}
	decoder := json.NewDecoder(&output)
	for index := range 2 {
		var record struct {
			Message   string `json:"msg"`
			Service   string `json:"service"`
			Instance  string `json:"instance"`
			Framework string `json:"framework"`
			Function  string `json:"function"`
			RequestID string `json:"request_id"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Message != "framework.event" || record.Service != "api" || record.Instance != "framework-test" || record.Function == "" {
			t.Fatalf("framework event = %+v", record)
		}
		if index == 0 && (record.Framework != "hertz" || record.RequestID != id.String()) {
			t.Fatalf("framework context = %+v", record)
		}
	}
}
