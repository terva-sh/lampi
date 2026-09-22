package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// hashPrefix marks a line that is already a SHA-256 of a device token.
// Anything else in the token file is the token itself and is hashed
// before it is written back.
const hashPrefix = "sha256:"

// Devices is the set of device-token hashes the lake will accept.
// The plaintext tokens are not retained.
type Devices struct {
	hashes [][32]byte
}

// HashToken is the SHA-256 hex of the bearer token string.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Empty reports whether no device is enrolled.
func (d *Devices) Empty() bool {
	return d == nil || len(d.hashes) == 0
}

// Len is the number of distinct device tokens enrolled.
func (d *Devices) Len() int {
	if d == nil {
		return 0
	}
	return len(d.hashes)
}

// Allow enrolls token by its hash. The token string is not stored.
func (d *Devices) Allow(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	var sum [32]byte
	copy(sum[:], mustHash(token))
	if d.has(sum) {
		return
	}
	d.hashes = append(d.hashes, sum)
}

// Match reports whether header is "Bearer <token>" for an enrolled
// device. The presented token is hashed and compared to the stored
// hashes. A mismatch fails closed.
func (d *Devices) Match(header string) bool {
	if d.Empty() {
		return false
	}
	const prefix = "Bearer "
	if len(header) < len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	got := header[len(prefix):]
	if got == "" {
		return false
	}
	sum := sha256.Sum256([]byte(got))
	ok := 0
	for i := range d.hashes {
		ok |= subtle.ConstantTimeCompare(sum[:], d.hashes[i][:])
	}
	return ok == 1
}

func (d *Devices) has(sum [32]byte) bool {
	for i := range d.hashes {
		if subtle.ConstantTimeCompare(sum[:], d.hashes[i][:]) == 1 {
			return true
		}
	}
	return false
}

func mustHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// LoadDevices reads path as one tenant's device tokens and rewrites any
// plaintext so the file contains only sha256:<hex> lines. path may be a
// file with one token per line, or a directory with one file per device.
//
// The client's copy of the token must be a different file. This rewrite
// replaces the plaintext in path.
func LoadDevices(path string) (*Devices, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if st.IsDir() {
		return loadDeviceDir(path)
	}
	return loadDeviceFile(path)
}

func loadDeviceDir(dir string) (*Devices, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	out := &Devices{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		one, err := loadDeviceFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, h := range one.hashes {
			if out.has(h) {
				continue
			}
			out.hashes = append(out.hashes, h)
		}
	}
	if out.Empty() {
		return nil, fmt.Errorf("auth: %s has no device tokens", dir)
	}
	return out, nil
}

func loadDeviceFile(path string) (*Devices, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	out := &Devices{}
	raw := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, hashPrefix) {
			h := strings.ToLower(strings.TrimPrefix(line, hashPrefix))
			if !hex64(h) {
				return nil, fmt.Errorf("auth: invalid token hash in %s", path)
			}
			sum, err := hex.DecodeString(h)
			if err != nil {
				return nil, fmt.Errorf("auth: invalid token hash in %s", path)
			}
			var fixed [32]byte
			copy(fixed[:], sum)
			if !out.has(fixed) {
				out.hashes = append(out.hashes, fixed)
			}
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, fmt.Errorf("auth: token in %s must be a single token, not a sentence", path)
		}
		raw = true
		out.Allow(line)
	}
	if out.Empty() {
		return nil, fmt.Errorf("auth: token file %s is empty", path)
	}
	canonical := canonicalHashes(out)
	if raw || string(b) != canonical {
		if err := writeAtomic(path, []byte(canonical), 0o600); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func canonicalHashes(d *Devices) string {
	lines := make([]string, len(d.hashes))
	for i, h := range d.hashes {
		lines[i] = hashPrefix + hex.EncodeToString(h[:])
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n"
}

func hex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	tmpName = ""
	return nil
}
