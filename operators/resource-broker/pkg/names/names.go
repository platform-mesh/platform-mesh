// Package names provides name transformation functions.
package names

import (
	"crypto/sha256"
	"encoding/hex"
)

// Hash returns a hex-encoded hash of the parts, truncated to 16 characters.
func Hash(parts ...string) string {
	h := sha256.New()
	for i, part := range parts {
		if i > 0 {
			// Join with a null byte so "ab","c" and "a","bc" produce
			// different hashes.
			h.Write([]byte{0})
		}
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
