package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCommandReportClassifiesWrappedDeadlineWithoutExposingCause(t *testing.T) {
	private := errors.New("password=private-test-value")
	cause := &maskedCommandCause{cause: errors.Join(context.DeadlineExceeded, private)}
	var output bytes.Buffer
	if code := report(&output, "service failed", cause, 1); code != 1 {
		t.Fatal("diagnostic changed process exit status")
	}
	var failure commandFailure
	if err := json.Unmarshal(output.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.CauseKind != "deadline_exceeded" || strings.Contains(output.String(), "private-test-value") {
		t.Fatal("wrapped native timeout was hidden or its private cause leaked")
	}
}

type maskedCommandCause struct{ cause error }

func (*maskedCommandCause) Error() string   { return "owned worker failed" }
func (e *maskedCommandCause) Unwrap() error { return e.cause }
