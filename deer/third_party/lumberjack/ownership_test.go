package lumberjack

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestRotationCompletesCompressionBeforeReturning(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		Filename:   filepath.Join(t.TempDir(), "node.log"),
		MaxSize:    1,
		MaxBackups: 2,
		Compress:   true,
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := logger.Write([]byte("first record\n")); err != nil {
		t.Fatal(err)
	}
	if err := logger.Rotate(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(logger.Filename), "node-*.log.gz"))
	if err != nil || len(files) != 1 {
		t.Fatalf("synchronous compressed backups = %v, %v; want one", files, err)
	}
	file, err := os.Open(files[0])
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(reader)
	err = errors.Join(readErr, reader.Close(), file.Close())
	if err != nil || string(data) != "first record\n" {
		t.Fatalf("compressed backup = %q, %v", data, err)
	}
}

func TestCloseIsTerminalAndIdempotent(t *testing.T) {
	t.Parallel()
	logger := &Logger{Filename: filepath.Join(t.TempDir(), "node.log")}
	if _, err := logger.Write([]byte("record\n")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := logger.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := logger.Write([]byte("after close\n")); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Write after Close = %v; want ErrClosedPipe", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRapidRotationsNeverOverwriteBackups(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		Filename:   filepath.Join(t.TempDir(), "node.log"),
		MaxSize:    1,
		MaxBackups: 32,
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	for index := range 12 {
		if _, err := fmt.Fprintf(logger, "record-%d\n", index); err != nil {
			t.Fatal(err)
		}
		if err := logger.Rotate(); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(logger.Filename), "node-*.log"))
	if err != nil || len(files) != 12 {
		t.Fatalf("rapid backups = %v, %v; want 12 unique backups", files, err)
	}
}

func TestRetentionRemovesExpiredAndExcessFiles(t *testing.T) {
	t.Parallel()
	t.Run("count retention", func(t *testing.T) { testRetention(t, 1, 1) })
	t.Run("age retention", func(t *testing.T) { testRetention(t, 10, 2) })
}

func testRetention(t *testing.T, maxBackups, expected int) {
	t.Helper()
	directory := t.TempDir()
	for _, age := range []time.Duration{48 * time.Hour, 2 * time.Hour, time.Hour} {
		name := "node-" + time.Now().UTC().Add(-age).Format("2006-01-02T15-04-05.000") + ".log"
		if err := os.WriteFile(filepath.Join(directory, name), []byte("old\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	logger := &Logger{
		Filename:   filepath.Join(directory, "node.log"),
		MaxSize:    1,
		MaxAge:     1,
		MaxBackups: maxBackups,
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := logger.Write([]byte("current\n")); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(directory, "node-*.log"))
	if err != nil || len(files) != expected {
		t.Fatalf("retained backups = %v, %v; want %d", files, err, expected)
	}
}

func TestRotationRejectsDirectoriesWithoutMovingContents(t *testing.T) {
	t.Parallel()
	directory := filepath.Join(t.TempDir(), "active")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(directory, "existing-data.txt")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	logger := &Logger{Filename: directory}
	err := logger.Rotate()
	closeErr := logger.Close()
	if err == nil {
		t.Error("Rotate must reject a directory as its active log")
	}
	if closeErr != nil {
		t.Error(closeErr)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("existing directory contents moved or changed: %q, %v", data, err)
	}
}

func TestSizeRotationAndOversizeWrite(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		Filename:   filepath.Join(t.TempDir(), "node.log"),
		MaxSize:    1,
		MaxBackups: 2,
	}
	t.Cleanup(func() {
		if err := logger.Close(); err != nil {
			t.Error(err)
		}
	})
	chunk := bytes.Repeat([]byte("a"), 600*1024)
	for range 2 {
		if _, err := logger.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if count, err := logger.Write(bytes.Repeat([]byte("x"), 1024*1024+1)); err == nil || count != 0 {
		t.Fatalf("oversize Write = %d, %v; want 0/error", count, err)
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(logger.Filename), "node-*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("size backups = %v, %v; want one", files, err)
	}
	for _, name := range append(files, logger.Filename) {
		data, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(data, chunk) {
			t.Fatalf("size-rotated contents %s = %d bytes, %v", name, len(data), err)
		}
	}
}

func TestConcurrentWritesRemainWholeRecords(t *testing.T) {
	t.Parallel()
	logger := &Logger{
		Filename: filepath.Join(t.TempDir(), "node.log"),
		MaxSize:  1,
	}
	var writers sync.WaitGroup
	for index := range 8 {
		writers.Go(func() {
			for sequence := range 20 {
				if _, err := fmt.Fprintf(logger, "record-%d-%d\n", index, sequence); err != nil {
					t.Error(err)
				}
			}
		})
	}
	writers.Wait()
	if err := logger.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logger.Filename)
	if err != nil || len(strings.Fields(string(data))) != 160 {
		t.Fatalf("concurrent records = %d bytes, %v", len(data), err)
	}
}
