package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// hashPrefix marks a line that is already a SHA-256 of a device token.
// A plaintext line is the token itself and is hashed before it is
// written back.
const hashPrefix = "sha256:"

// TokenSuffix is the name a device file needs in a token directory.
// Other files there (an editor's laptop~, a laptop.revoked) are not
// loaded.
const TokenSuffix = ".token"

// Devices is the set of device-token hashes the lake will accept.
// The plaintext tokens are not retained. It is safe for concurrent
// use: Replace swaps the set under requests that are matching.
type Devices struct {
	mu     sync.RWMutex
	hashes [][32]byte
	// Ignored lists files in a token directory that were not loaded
	// because they do not end in TokenSuffix.
	Ignored []string
}

// HashToken is the SHA-256 hex of the bearer token string.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Empty reports whether no device is enrolled.
func (d *Devices) Empty() bool {
	return d.Len() == 0
}

// Len is the number of distinct device tokens enrolled.
func (d *Devices) Len() int {
	if d == nil {
		return 0
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.hashes)
}

// Allow enrolls token by its hash. The token string is not stored.
func (d *Devices) Allow(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.addLocked(sha256.Sum256([]byte(token)))
}

// Replace makes d accept exactly the tokens next accepts. A request
// already past Match is not affected; the next one sees the new set.
func (d *Devices) Replace(next *Devices) {
	next.mu.RLock()
	hashes := append([][32]byte(nil), next.hashes...)
	next.mu.RUnlock()
	d.mu.Lock()
	d.hashes = hashes
	d.mu.Unlock()
}

// Match reports whether header is "Bearer <token>" for an enrolled
// device. The presented token is hashed and compared to the stored
// hashes. A mismatch fails closed.
func (d *Devices) Match(header string) bool {
	if d == nil {
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
	d.mu.RLock()
	defer d.mu.RUnlock()
	ok := 0
	for i := range d.hashes {
		ok |= subtle.ConstantTimeCompare(sum[:], d.hashes[i][:])
	}
	return ok == 1
}

func (d *Devices) addLocked(sum [32]byte) {
	for i := range d.hashes {
		if subtle.ConstantTimeCompare(sum[:], d.hashes[i][:]) == 1 {
			return
		}
	}
	d.hashes = append(d.hashes, sum)
}

// LoadDevices reads path as one tenant's device tokens and rewrites any
// plaintext line to its sha256:<hex> line. path may be a file with one
// token per line, or a directory with one <name>.token file per device.
//
// A plaintext token is 64 lowercase hex characters, which is what
// terva-lampi login writes. A line starting with # is a comment and is
// kept, in place, through the rewrite. Any other line is an error that
// names the file and line, not the line's text.
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
	out := &Devices{}
	if err := loadDeviceFile(path, out); err != nil {
		return nil, err
	}
	if out.Empty() {
		return nil, fmt.Errorf("auth: token file %s has no device tokens", path)
	}
	return out, nil
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
		if !strings.HasSuffix(e.Name(), TokenSuffix) {
			out.Ignored = append(out.Ignored, e.Name())
			continue
		}
		if err := loadDeviceFile(filepath.Join(dir, e.Name()), out); err != nil {
			return nil, err
		}
	}
	if out.Empty() {
		msg := fmt.Sprintf("auth: %s has no device tokens; in a token directory each device is one <name>%s file", dir, TokenSuffix)
		if len(out.Ignored) > 0 {
			msg += fmt.Sprintf(", and %s do not end in %s", strings.Join(out.Ignored, ", "), TokenSuffix)
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// loadDeviceFile adds the tokens in path to out and rewrites the file
// when a line changed. Blank and comment lines stay where they were.
func loadDeviceFile(path string, out *Devices) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	lines := strings.Split(string(b), "\n")
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		var sum [32]byte
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			kept = append(kept, line)
			continue
		case strings.HasPrefix(line, hashPrefix):
			h := strings.ToLower(strings.TrimPrefix(line, hashPrefix))
			if !hex64(h) {
				return fmt.Errorf("auth: %s line %d: invalid token hash", path, i+1)
			}
			raw, _ := hex.DecodeString(h)
			copy(sum[:], raw)
		case hex64(line):
			sum = sha256.Sum256([]byte(line))
		default:
			return fmt.Errorf("auth: %s line %d is not a device token; a token is 64 lowercase hex characters, as terva-lampi login writes, and a comment line starts with #", path, i+1)
		}
		kept = append(kept, hashPrefix+hex.EncodeToString(sum[:]))
		out.mu.Lock()
		out.addLocked(sum)
		out.mu.Unlock()
	}
	canonical := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"
	if canonical != string(b) {
		if err := writeAtomic(path, []byte(canonical), 0o600); err != nil {
			return err
		}
	}
	return nil
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
