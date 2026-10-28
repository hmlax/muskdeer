//go:build integration && browser

package migrate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAdminBrowserUsesActualCLIAndAPI(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	_, base := nativeAdminFixture(ctx, t)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, ".cache", "admin-browser")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	artifacts, err := os.MkdirTemp(parent, "run-")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, "python3", filepath.Join(root, "scripts", "web.py"), "browser-test")
	command.Dir = root
	command.Env = append(os.Environ(),
		"MUSKDEER_BROWSER_ORIGIN="+base,
		"MUSKDEER_BROWSER_USERNAME=native-viewer",
		"MUSKDEER_BROWSER_PASSWORD="+adminFixturePassword,
		"MUSKDEER_BROWSER_ARTIFACTS="+artifacts,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("actual administration browser failed: %v\n%s", err, output)
	}
	t.Logf("actual browser passed; screenshots=%s", artifacts)
}
