// Package requestid carries bounded correlation metadata, never authorization.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
)

const (
	// Header is shared by HTTP and gRPC metadata; HTTP names are case-insensitive.
	Header = "x-request-id"
	// Attribute is the protected root JSON log field.
	Attribute = "request_id"
)

type key struct{}

// ID is a validated, bounded correlation value. Its zero value means absent.
type ID struct {
	value string
}

// New accepts 1..64 ASCII letters/digits/dot/underscore/hyphen, otherwise
// generates 128 random bits as lowercase hex. It never echoes an invalid value.
func New(ctx context.Context, candidate string) (ID, error) {
	if ctx == nil {
		return ID{}, errors.New("request ID context is required")
	}
	if err := ctx.Err(); err != nil {
		return ID{}, err
	}
	if valid(candidate) {
		return ID{value: candidate}, nil
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ID{}, err
	}
	if err := ctx.Err(); err != nil {
		return ID{}, err
	}
	return ID{value: hex.EncodeToString(random[:])}, nil
}

func (id ID) String() string { return id.value }

// Bind preserves the parent's cancellation/deadline and adds typed metadata.
func Bind(ctx context.Context, id ID) context.Context { return context.WithValue(ctx, key{}, id) }

// FromContext returns absent metadata for a nil or unbound context.
func FromContext(ctx context.Context) ID {
	if ctx == nil {
		return ID{}
	}
	id, _ := ctx.Value(key{}).(ID)
	return id
}

func valid(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if ch != '-' && ch != '_' && ch != '.' && (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}
