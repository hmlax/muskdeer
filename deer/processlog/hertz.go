package processlog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

type hertzLogger struct {
	bridge *bridge
	level  atomic.Int32
}

var _ hlog.FullLogger = (*hertzLogger)(nil)

func (l *hertzLogger) SetOutput(io.Writer) {
	l.bridge.record(errors.New("framework output is owned by service logging configuration"))
}

func (l *hertzLogger) SetLevel(level hlog.Level) {
	if level < hlog.LevelTrace || level > hlog.LevelFatal {
		l.bridge.record(errors.New("invalid Hertz log level"))
		return
	}
	l.level.Store(int32(level))
}

func (l *hertzLogger) emit(ctx context.Context, level hlog.Level, count int) {
	if int32(level) < l.level.Load() && level != hlog.LevelFatal {
		return
	}
	severity := slog.LevelInfo
	switch level {
	case hlog.LevelTrace, hlog.LevelDebug:
		severity = slog.LevelDebug
	case hlog.LevelWarn:
		severity = slog.LevelWarn
	case hlog.LevelError, hlog.LevelFatal:
		severity = slog.LevelError
	}
	if err := l.bridge.emit(ctx, severity, "hertz", count); err != nil {
		return
	}
}

func (l *hertzLogger) Trace(values ...any) {
	l.emit(context.Background(), hlog.LevelTrace, len(values))
}

func (l *hertzLogger) Tracef(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelTrace, len(values))
}

func (l *hertzLogger) CtxTracef(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelTrace, len(values))
}

func (l *hertzLogger) Debug(values ...any) {
	l.emit(context.Background(), hlog.LevelDebug, len(values))
}

func (l *hertzLogger) Debugf(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelDebug, len(values))
}

func (l *hertzLogger) CtxDebugf(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelDebug, len(values))
}

func (l *hertzLogger) Info(values ...any) {
	l.emit(context.Background(), hlog.LevelInfo, len(values))
}

func (l *hertzLogger) Infof(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelInfo, len(values))
}

func (l *hertzLogger) CtxInfof(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelInfo, len(values))
}

func (l *hertzLogger) Notice(values ...any) {
	l.emit(context.Background(), hlog.LevelNotice, len(values))
}

func (l *hertzLogger) Noticef(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelNotice, len(values))
}

func (l *hertzLogger) CtxNoticef(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelNotice, len(values))
}

func (l *hertzLogger) Warn(values ...any) {
	l.emit(context.Background(), hlog.LevelWarn, len(values))
}

func (l *hertzLogger) Warnf(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelWarn, len(values))
}

func (l *hertzLogger) CtxWarnf(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelWarn, len(values))
}

func (l *hertzLogger) Error(values ...any) {
	l.emit(context.Background(), hlog.LevelError, len(values))
}

func (l *hertzLogger) Errorf(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelError, len(values))
}

func (l *hertzLogger) CtxErrorf(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelError, len(values))
}

func (l *hertzLogger) Fatal(values ...any) {
	l.emit(context.Background(), hlog.LevelFatal, len(values))
	os.Exit(1)
}

func (l *hertzLogger) Fatalf(_ string, values ...any) {
	l.emit(context.Background(), hlog.LevelFatal, len(values))
	os.Exit(1)
}

func (l *hertzLogger) CtxFatalf(ctx context.Context, _ string, values ...any) {
	l.emit(ctx, hlog.LevelFatal, len(values))
	os.Exit(1)
}
