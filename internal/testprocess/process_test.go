package testprocess

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hmlax/muskdeer/deer/nacos"
)

type configReadFunc func(context.Context, string) ([]byte, error)

func (f configReadFunc) GetConfig(ctx context.Context, dataID string) ([]byte, error) {
	return f(ctx, dataID)
}

func TestCommandRejectsRemovedHTTPApplications(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"api", "admin", "adminuser"} {
		command, cleanup, err := Command(t.Context(), name, "unused.json")
		if err == nil || command != nil || cleanup != nil {
			t.Fatalf("Command(%q) = (%v, %v); want rejection before opening resources", name, command, err)
		}
	}
}

func TestPublishedConfigWaitsForVisibilityWithoutChangingContent(t *testing.T) {
	t.Parallel()
	expected := []byte("test-only-configuration")
	calls := 0
	read := configReadFunc(func(_ context.Context, dataID string) ([]byte, error) {
		if dataID != "test.json" {
			t.Fatalf("read data ID = %q; want test.json", dataID)
		}
		calls++
		if calls <= 2 {
			return nil, &nacos.APIError{HTTPStatus: 404}
		}
		return expected, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := waitPublishedConfig(ctx, read, "test.json", expected); err != nil || calls != 3 {
		t.Fatalf("config visibility = %v after %d reads; want success after two missing reads", err, calls)
	}
}

func TestPublishedConfigRejectsOtherFailuresAndUnexpectedContent(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{&nacos.APIError{HTTPStatus: 403}, context.DeadlineExceeded, nil} {
		calls := 0
		read := configReadFunc(func(context.Context, string) ([]byte, error) {
			calls++
			return []byte("private-unexpected-value"), failure
		})
		err := waitPublishedConfig(t.Context(), read, "test.json", []byte("expected"))
		if err == nil || calls != 1 || strings.Contains(err.Error(), "private-unexpected-value") {
			t.Fatalf("config visibility = %v after %d calls; want immediate safe failure", err, calls)
		}
		if failure != nil && !errors.Is(err, failure) {
			t.Fatalf("config visibility = %v; want original error %v", err, failure)
		}
	}
}

func TestPublishedConfigJoinsCallerCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	read := configReadFunc(func(context.Context, string) ([]byte, error) {
		calls++
		cancel()
		return nil, &nacos.APIError{HTTPStatus: 404}
	})
	if err := waitPublishedConfig(ctx, read, "test.json", []byte("expected")); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled config visibility = %v after %d calls; want caller cancellation", err, calls)
	}
}
