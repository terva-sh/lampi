// Package auth is the device-token file on the client and the bearer
// check on the server.
//
// Production storage is a hash at rest on the server, never the token.
// This scaffold compares the file contents in plaintext so a loopback
// lake can be tried without an account system. Do not point it at a
// network until that changes.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Generate returns a 256-bit token encoded as 64 hex characters.
func Generate() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Write stores token at path, mode 0600. The directory is created 0700.
// A trailing newline is written so the file is a normal text file; Read
// strips it. The token itself must be a single line.
func Write(path, token string) error {
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return fmt.Errorf("auth: token must be a single non-empty line")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	return nil
}

// Read loads a token file. Surrounding whitespace is ignored.
func Read(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	token := strings.TrimSpace(string(b))
	if token == "" {
		return "", fmt.Errorf("auth: token file %s is empty", path)
	}
	return token, nil
}

// Match reports whether header is "Bearer <token>". The comparison is
// constant-time when the lengths match. A mismatch of length fails closed.
func Match(header, token string) bool {
	const prefix = "Bearer "
	if len(header) < len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(token) || token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}
