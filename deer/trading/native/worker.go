package native

import (
	"crypto/sha256"
	"encoding/hex"
)

// WorkerSymbol preserves existing ASCII labels and bounds non-ASCII catalog
// names to 25 safe characters. It is only an internal lifecycle label: trading
// symbols, RPC routing, source selection and order identities stay unchanged.
func WorkerSymbol(symbol string) string {
	for _, character := range symbol {
		if character > 127 {
			digest := sha256.Sum256([]byte(symbol))
			return "u" + hex.EncodeToString(digest[:12])
		}
	}
	return symbol
}
