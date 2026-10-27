// Package processlog installs SDK logging only at a sole process entry, before
// any SDK use. Generic Deer constructors never install these global adapters.
package processlog

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	kratoslog "github.com/go-kratos/kratos/v2/log"

	"github.com/hmlax/muskdeer/deer"
	"github.com/hmlax/muskdeer/deer/logging"
)

type bridge struct {
	target *logging.Logger
	mu     sync.Mutex
	err    error
}

type kratosLogger struct {
	bridge *bridge
}

// Install replaces SDK globals before workers/listeners start. The caller must
// be the only application in this process and join SDK users before cleanup.
// Restore runs before Deer closes its logger, including startup rollback.
func Install(ctx context.Context, target *logging.Logger) (deer.Cleanup, error) {
	if ctx == nil || target == nil {
		return nil, errors.New("process logging requires a context and owned logger")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	previousKratos := kratoslog.GetLogger()
	previousHertz := hlog.DefaultLogger()
	shared := &bridge{target: target}
	kratoslog.SetLogger(&kratosLogger{bridge: shared})
	hlog.SetLogger(&hertzLogger{bridge: shared})
	var once sync.Once
	return func(context.Context) error {
		once.Do(func() {
			// Public setters are deliberately used only after owned SDK work joins.
			kratoslog.SetLogger(previousKratos)
			hlog.SetLogger(previousHertz)
		})
		shared.mu.Lock()
		defer shared.mu.Unlock()
		return shared.err
	}, nil
}

func (b *bridge) record(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err == nil {
		b.err = err
	}
}

func (b *bridge) emit(ctx context.Context, level slog.Level, library string, argumentCount int) error {
	// SDK format strings/arguments can embed request bodies or credentials.
	// Keep severity and a static source location; business/lifecycle events use
	// explicit safe fields rather than serializing third-party argument values.
	function, line := source()
	err := b.target.LogAttrs(ctx, level, "framework.event",
		slog.String("framework", library),
		slog.String("function", function),
		slog.Int("line", line),
		slog.Int("argument_count", argumentCount))
	b.record(err)
	return err
}

func (l *kratosLogger) Log(level kratoslog.Level, keyvals ...any) error {
	var severity slog.Level
	switch level {
	case kratoslog.LevelDebug:
		severity = slog.LevelDebug
	case kratoslog.LevelInfo:
		severity = slog.LevelInfo
	case kratoslog.LevelWarn:
		severity = slog.LevelWarn
	case kratoslog.LevelError, kratoslog.LevelFatal:
		severity = slog.LevelError
	default:
		err := errors.New("unsupported framework log level")
		l.bridge.record(err)
		return err
	}
	return l.bridge.emit(context.Background(), severity, "kratos", len(keyvals))
}

func source() (string, int) {
	var callers [16]uintptr
	count := runtime.Callers(3, callers[:])
	frames := runtime.CallersFrames(callers[:count])
	for {
		frame, more := frames.Next()
		if !strings.Contains(frame.Function, "/internal/processlog.") && !strings.Contains(frame.Function, "/pkg/common/hlog.") {
			name := frame.Function
			if len(name) > 256 {
				name = name[:256]
			}
			return name, frame.Line
		}
		if !more {
			return "unknown", 0
		}
	}
}
