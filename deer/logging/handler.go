package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/shopspring/decimal"
)

// identityHandler prevents caller attributes from overriding root identity and
// standard JSON fields. The underlying handler owns the prebound root identity.
type identityHandler struct {
	next    slog.Handler
	grouped bool
}

var _ slog.Handler = (*identityHandler)(nil)

func (h *identityHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *identityHandler) Handle(ctx context.Context, record slog.Record) error {
	filtered := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		if protected, ok := protectAttribute(attr, !h.grouped); ok {
			filtered.AddAttrs(protected)
		}
		return true
	})
	return h.next.Handle(ctx, filtered)
}

func (h *identityHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	filtered := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		if protected, ok := protectAttribute(attr, !h.grouped); ok {
			filtered = append(filtered, protected)
		}
	}
	return &identityHandler{
		next:    h.next.WithAttrs(filtered),
		grouped: h.grouped,
	}
}

func protectAttribute(attr slog.Attr, root bool) (slog.Attr, bool) {
	if !root {
		return attr, true
	}
	if protectedKey(attr.Key) {
		return slog.Attr{}, false
	}
	attr.Value = attr.Value.Resolve()
	if attr.Key == "" && attr.Value.Kind() == slog.KindGroup {
		var children []slog.Attr
		for _, child := range attr.Value.Group() {
			if protected, ok := protectAttribute(child, true); ok {
				children = append(children, protected)
			}
		}
		return slog.GroupAttrs("", children...), true
	}
	return attr, true
}

func (h *identityHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := h.next
	if !h.grouped && protectedKey(name) {
		next = next.WithGroup("attributes")
	}
	return &identityHandler{
		next:    next.WithGroup(name),
		grouped: true,
	}
}

func protectedKey(key string) bool {
	switch strings.ToLower(key) {
	case "service", "instance", slog.TimeKey, slog.LevelKey, slog.MessageKey:
		return true
	default:
		return false
	}
}

func sensitiveKey(key string) bool {
	key = strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(key))
	switch key {
	case "password", "passwd", "secret", "clientsecret", "apikey", "apisecret", "token", "accesstoken", "refreshtoken", "authorization", "signature", "privatekey", "cookie", "setcookie", "credentials", "body", "requestbody", "responsebody", "payload":
		return true
	default:
		return false
	}
}

func redactAttribute(groups []string, attr slog.Attr) slog.Attr {
	if sensitiveKey(attr.Key) {
		return slog.String(attr.Key, redacted)
	}
	for _, group := range groups {
		if sensitiveKey(group) {
			return slog.String(attr.Key, redacted)
		}
	}
	if attr.Value.Kind() == slog.KindAny {
		switch value := attr.Value.Any().(type) {
		case slog.Level:
			return slog.String(attr.Key, value.String())
		case decimal.Decimal:
			return slog.String(attr.Key, value.String())
		case error:
			return slog.String(attr.Key, fmt.Sprint(value))
		default:
			return slog.String(attr.Key, unsupported)
		}
	}
	return attr
}
