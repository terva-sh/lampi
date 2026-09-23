package adapter

import (
	"crypto/sha256"
	"encoding/hex"
)

// CWDHash is the project bucket terva already uses: hex(sha256(cwd)[:8]).
// The absolute path string is the input, so the same repo in two
// directories does not share a hash. Other harnesses use the same
// function so an allow rule written against that hash still matches.
func CWDHash(cwd string) string {
	if cwd == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(sum[:8])
}
