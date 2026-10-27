// Package logging provides instance-owned JSON logs and synchronous file rotation.
package logging

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	kratoslog "github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/proto"

	configv1 "github.com/hmlax/muskdeer/deer/proto/config/v1"
	"github.com/hmlax/muskdeer/third_party/lumberjack"
)

// LevelDebug, LevelInfo, LevelWarn and LevelError are accepted configuration levels.
const (
	LevelDebug  = "debug"
	LevelInfo   = "info"
	LevelWarn   = "warn"
	LevelError  = "error"
	redacted    = "[REDACTED]"
	unsupported = "[UNSUPPORTED]"
)

// Identity identifies the service and instance in every root log record.
type Identity struct {
	Service  string
	Instance string
}

// Logger owns its file, lock and output error state. It never changes global loggers.
// Prefer LogAttrs when the caller must receive write errors immediately. Errors
// discarded by slog's API are retained by Close as the first output failure.
type Logger struct {
	handler  slog.Handler
	writer   *ownedWriter
	filename string
}

type ownedWriter struct {
	mu       sync.Mutex
	outputs  []io.Writer
	file     *lumberjack.Logger
	lock     *os.File
	closed   bool
	firstErr error
	closeErr error
}

var _ kratoslog.Logger = (*Logger)(nil)

// New validates configuration before opening outputs. A nil config selects INFO
// on stdout; a supplied config must enable stdout or a fully configured file.
// stdout is borrowed, never closed; nil selects os.Stdout when stdout is enabled.
func New(ctx context.Context, cfg *configv1.Logging, identity Identity, stdout io.Writer) (*Logger, error) {
	if ctx == nil {
		return nil, errors.New("logging context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if identity.Service == "" || identity.Instance == "" {
		return nil, errors.New("logging service and instance are required")
	}
	if cfg == nil {
		cfg = &configv1.Logging{
			Level:  LevelInfo,
			Stdout: true,
		}
	} else {
		cfg = proto.CloneOf(cfg)
	}
	level, err := validate(cfg)
	if err != nil {
		return nil, err
	}
	writer := &ownedWriter{}
	var filename string
	if cfg.File != nil {
		filename, err = instanceFilename(cfg.File.Filename, identity)
		if err != nil {
			return nil, err
		}
		if err := writer.openFile(cfg.File, filename); err != nil {
			return nil, errors.Join(err, writer.Close())
		}
	}
	if cfg.Stdout {
		if stdout == nil {
			stdout = os.Stdout
		}
		writer.outputs = append(writer.outputs, stdout)
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	base := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactAttribute,
	}).WithAttrs([]slog.Attr{
		slog.String("service", identity.Service),
		slog.String("instance", identity.Instance),
	})
	return &Logger{
		handler:  &identityHandler{next: base},
		writer:   writer,
		filename: filename,
	}, nil
}

// Slog returns a structured logger sharing this instance's outputs and lifetime.
func (l *Logger) Slog() *slog.Logger { return slog.New(l.handler) }

// Filename returns the instance-scoped log path, or empty for stdout-only logs.
func (l *Logger) Filename() string { return l.filename }

// LogAttrs writes one structured event and returns any output error.
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) error {
	if ctx == nil {
		return errors.New("log record context is required")
	}
	if !l.handler.Enabled(ctx, level) {
		return nil
	}
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(attrs...)
	return l.handler.Handle(ctx, record)
}

// Log implements the Kratos key/value logger interface without global adapters.
func (l *Logger) Log(level kratoslog.Level, keyvals ...any) error {
	if len(keyvals)%2 != 0 {
		return errors.New("log key/value pairs must be complete")
	}
	severity, err := kratosLevel(level)
	if err != nil {
		return err
	}
	var message string
	attrs := make([]slog.Attr, 0, len(keyvals)/2)
	for index := 0; index < len(keyvals); index += 2 {
		key, ok := keyvals[index].(string)
		if !ok || key == "" {
			return errors.New("log keys must be nonempty strings")
		}
		if key == slog.MessageKey {
			message, ok = keyvals[index+1].(string)
			if !ok {
				return errors.New("log message must be a string")
			}
			continue
		}
		attrs = append(attrs, slog.Any(key, keyvals[index+1]))
	}
	return l.LogAttrs(context.Background(), severity, message, attrs...)
}

// Close releases this instance's file/lock and preserves earlier output errors.
// It is safe to repeat or call concurrently. Borrowed stdout remains open.
func (l *Logger) Close() error { return l.writer.Close() }

func (w *ownedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	var result error
	for _, output := range w.outputs {
		count, err := output.Write(data)
		if count != len(data) && err == nil {
			err = io.ErrShortWrite
		}
		result = errors.Join(result, err)
	}
	if result != nil {
		if w.firstErr == nil {
			w.firstErr = result
		}
		return 0, result
	}
	return len(data), nil
}

func (w *ownedWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.closed {
		w.closed = true
		w.closeErr = w.firstErr
		if w.file != nil {
			w.closeErr = errors.Join(w.closeErr, w.file.Close())
		}
		if w.lock != nil {
			w.closeErr = errors.Join(w.closeErr, w.lock.Close())
		}
	}
	return w.closeErr
}

func (w *ownedWriter) openFile(cfg *configv1.LogFile, filename string) error {
	if err := os.MkdirAll(filepath.Dir(filename), 0750); err != nil {
		return err
	}
	lock, err := os.OpenFile(filename+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	w.lock = lock
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("lock log file %q: %w", filename, err)
	}
	w.file = &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    int(cfg.MaxSizeMb),
		MaxBackups: int(cfg.MaxBackups),
		MaxAge:     int(cfg.MaxAgeDays),
		Compress:   cfg.Compress,
	}
	if _, err := w.file.Write(nil); err != nil {
		return err
	}
	w.outputs = append(w.outputs, w.file)
	return nil
}

// Validate checks outputs and level without creating a writer. Nil uses INFO stdout.
func Validate(cfg *configv1.Logging) (slog.Level, error) {
	if cfg == nil {
		return slog.LevelInfo, nil
	}
	return validate(cfg)
}

func validate(cfg *configv1.Logging) (slog.Level, error) {
	if !cfg.Stdout && cfg.File == nil {
		return 0, errors.New("logging requires at least one output")
	}
	if file := cfg.File; file != nil {
		if strings.TrimSpace(file.Filename) == "" || file.MaxSizeMb <= 0 || file.MaxSizeMb > 1024 || file.MaxBackups <= 0 || file.MaxAgeDays <= 0 || file.MaxAgeDays > 36500 {
			return 0, errors.New("log file requires filename, max_size_mb in 1..1024, positive max_backups and max_age_days in 1..36500")
		}
	}
	switch strings.ToLower(cfg.Level) {
	case "", LevelInfo:
		return slog.LevelInfo, nil
	case LevelDebug:
		return slog.LevelDebug, nil
	case LevelWarn:
		return slog.LevelWarn, nil
	case LevelError:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported log level %q", cfg.Level)
	}
}

func instanceFilename(base string, identity Identity) (string, error) {
	for _, segment := range []string{identity.Service, identity.Instance} {
		if len(segment) > 128 || segment == "." || segment == ".." {
			return "", errors.New("invalid log identity path segment")
		}
		for _, char := range segment {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
				continue
			}
			return "", errors.New("file logging identity permits only ASCII letters, digits, dot, underscore and hyphen")
		}
	}
	return filepath.Join(filepath.Dir(base), identity.Service, identity.Instance, filepath.Base(base)), nil
}

func kratosLevel(level kratoslog.Level) (slog.Level, error) {
	switch level {
	case kratoslog.LevelDebug:
		return slog.LevelDebug, nil
	case kratoslog.LevelInfo:
		return slog.LevelInfo, nil
	case kratoslog.LevelWarn:
		return slog.LevelWarn, nil
	case kratoslog.LevelError, kratoslog.LevelFatal:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unsupported Kratos log level %d", level)
	}
}
