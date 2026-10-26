package logging

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONSizeRotationPreservesEveryRecord(t *testing.T) {
	t.Parallel()
	cfg := fileConfig(t)
	logger, err := New(t.Context(), cfg, Identity{
		Service:  "execution",
		Instance: "rotation",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	const records = 600
	padding := strings.Repeat("x", 4000)
	for sequence := range records {
		if err := logger.LogAttrs(t.Context(), slog.LevelInfo, "rotation record", slog.Int("sequence", sequence), slog.String("padding", padding)); err != nil {
			t.Fatal(err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(logger.Filename()), "service-*.log.gz"))
	if err != nil || len(backups) != 2 {
		t.Fatalf("compressed JSON backups = %v, %v; want 2", backups, err)
	}
	seen := make(map[int]struct{}, records)
	for _, name := range append(backups, logger.Filename()) {
		file, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		var source io.Reader = file
		var compressed *gzip.Reader
		if strings.HasSuffix(name, ".gz") {
			compressed, err = gzip.NewReader(file)
			if err != nil {
				t.Fatal(err)
			}
			source = compressed
		}
		scanner := bufio.NewScanner(source)
		for scanner.Scan() {
			var record struct {
				Sequence int    `json:"sequence"`
				Message  string `json:"msg"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if _, duplicate := seen[record.Sequence]; duplicate || record.Sequence < 0 || record.Sequence >= records || record.Message != "rotation record" {
				t.Fatalf("corrupted/duplicate rotated record: %+v", record)
			}
			seen[record.Sequence] = struct{}{}
		}
		err = scanner.Err()
		if compressed != nil {
			err = errors.Join(err, compressed.Close())
		}
		if err := errors.Join(err, file.Close()); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != records {
		t.Fatalf("rotated JSON records = %d, want %d", len(seen), records)
	}
}
