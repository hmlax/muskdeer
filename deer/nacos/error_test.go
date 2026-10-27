package nacos

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAPIErrorReportsSafeFailureCategoryWithoutRawRequest(t *testing.T) {
	cause := errors.Join(context.DeadlineExceeded, errors.New("sensitive accessToken=do-not-log"))
	err := &APIError{
		HTTPStatus: 503,
		Code:       500,
		operation:  "lookup",
		cause:      cause,
	}
	message := err.Error()
	if !strings.Contains(message, "http=503 code=500 deadline=true canceled=false") || strings.Contains(message, "accessToken") || strings.Contains(message, "do-not-log") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unsafe or incomplete diagnostic category: %s", message)
	}
}
