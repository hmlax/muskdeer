package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

func MakeTxCleanup(tx *Tx, result *error) func() {
	return func() {
		if recovered := recover(); recovered != nil {
			if err := tx.Rollback(); err != nil {
				slog.ErrorContext(context.Background(), "rollback after panic", "error", err)
			}
			panic(recovered)
		}
		if result != nil && *result != nil {
			*result = errors.Join(*result, tx.Rollback())
			return
		}
		if err := tx.Commit(); err != nil {
			err = errors.Join(fmt.Errorf("commit transaction: %w", err), tx.Rollback())
			if result != nil {
				*result = err
				return
			}
			slog.ErrorContext(context.Background(), "commit transaction", "error", err)
		}
	}
}
func ComputeTreePath(parentPath string, nodeID uint32) string {
	if parentPath == "" {
		return "/"
	}
	return strings.TrimSuffix(parentPath, "/") + "/" + strconv.FormatUint(uint64(nodeID), 10) + "/"
}
