//go:build !darwin && !linux

package logging

import (
	"errors"
	"os"
)

func lockFile(*os.File) error {
	return errors.New("file logging requires Linux or macOS file ownership locking")
}
