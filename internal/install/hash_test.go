package install

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Of is the digest of a string, for building manifest fixtures that
// match content written into a test game directory.
func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
