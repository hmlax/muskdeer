//go:build integration

package migrate

import (
	"context"
	"os"
	"testing"

	"github.com/hmlax/muskdeer/deer/logging"
)

func nativeWorkerLogger(ctx context.Context, t *testing.T, owner string) *logging.Logger {
	t.Helper()
	path := t.TempDir() + "/native.log"
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	logger, err := logging.New(ctx, nil, logging.Identity{
		Service:  "execution",
		Instance: owner,
	}, file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
		if t.Failed() {
			content, err := os.ReadFile(path)
			if err != nil {
				t.Error(err)
				return
			}
			if len(content) > 6000 {
				content = content[len(content)-6000:]
			}
			t.Logf("recent native worker diagnostics: %s", content)
		}
	})
	return logger
}
