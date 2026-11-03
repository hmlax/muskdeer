package domain

import (
	"crypto/sha256"
	"encoding/hex"
)

// IntentClientOrderID is the immutable execution identity of an account/key
// pair. Keep this formula stable across callers, execution versions and owners.
func IntentClientOrderID(accountID, idempotencyKey string) (string, error) {
	if !intentIdentityPart(accountID, 64) || !intentIdentityPart(idempotencyKey, 128) {
		return "", ErrInvalidOrder
	}
	digest := sha256.Sum256([]byte(accountID + "\n" + idempotencyKey))
	return hex.EncodeToString(digest[:]), nil
}

func intentIdentityPart(value string, maximum int) bool {
	if len(value) == 0 || len(value) > maximum {
		return false
	}
	for index := range len(value) {
		c := value[index]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':' {
			continue
		}
		return false
	}
	return true
}
