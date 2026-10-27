// Package ownerid creates a new process incarnation identity, distinct from
// request correlation and from a reusable Nacos service instance name.
package ownerid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

// New appends 128 random bits to a bounded ASCII instance name. Call once per
// process incarnation and retain it across that process's acquisition retries.
func New(ctx context.Context, instance string) (string, error) {
	if ctx == nil {
		return "", errors.New("owner identity context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(instance) == 0 || len(instance) > 64 {
		return "", errors.New("owner instance must contain 1..64 safe ASCII characters")
	}
	for index := range len(instance) {
		character := instance[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return "", errors.New("owner instance must contain 1..64 safe ASCII characters")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return instance + "." + hex.EncodeToString(nonce[:]), nil
}

// Valid checks the shape of an incarnation identity; it does not authenticate
// the holder or prove that a token still matches the authoritative lease.
func Valid(value string) bool {
	prefix, suffix, found := split(value)
	if !found || len(prefix) == 0 || len(prefix) > 64 || len(suffix) != 32 {
		return false
	}
	for index := range len(prefix) {
		character := prefix[index]
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.' {
			continue
		}
		return false
	}
	for index := range len(suffix) {
		character := suffix[index]
		if character < '0' || character > '9' {
			if character < 'a' || character > 'f' {
				return false
			}
		}
	}
	return true
}

// Instance extracts a validated process instance name. It neither authenticates
// the process nor proves current ownership; callers must read its live lease.
func Instance(value string) (string, bool) {
	if !Valid(value) {
		return "", false
	}
	prefix, _, _ := split(value)
	return prefix, true
}

func split(value string) (string, string, bool) {
	index := strings.LastIndexByte(value, '.')
	if index < 0 {
		return "", "", false
	}
	return value[:index], value[index+1:], true
}
