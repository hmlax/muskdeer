// Package testprocess selects independently built application and module fixtures.
package testprocess

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// Command never accepts a role switch. Every process has its own executable.
func Command(ctx context.Context, name, path string) *exec.Cmd {
	binary := name
	if name != "admin" && name != "operator" && os.Getenv("MUSKDEER_CORE_TEST_BINARIES") != "1" {
		binary = "module-" + name
	}
	return exec.CommandContext(ctx, filepath.Join("..", "..", ".cache", "bin", binary), "-config", path)
}
