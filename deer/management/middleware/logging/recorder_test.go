package logging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hmlax/muskdeer/deer/management/httpmeta"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

func TestNativeRecorderDoesNotPersistCredentialBodies(t *testing.T) {
	var recorded *operatorV1.ApiAuditLog
	recorder, err := NewRecorder(WithWriteApiLogFunc(func(_ context.Context, entry *operatorV1.ApiAuditLog) error {
		recorded = entry
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/access-keys:issue-token", strings.NewReader(`{"secretKey":"private-secret"}`))
	request.Header.Set("Content-Type", "application/json")
	exchange := &httpmeta.Exchange{
		RequestValue:  request,
		Response:      make(http.Header),
		OperationName: operatorV1.OperationAccessKeyServiceIssueToken,
		Route:         "/admin/v1/access-keys:issue-token",
	}
	ctx := recorder.Begin(t.Context(), exchange)
	recorder.Finish(ctx, exchange, nil, 0)
	if recorded == nil || recorded.GetRequestBody() != "" {
		t.Fatal("native request credentials entered API audit storage")
	}
}
