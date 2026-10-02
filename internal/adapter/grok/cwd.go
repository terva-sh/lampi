package grok

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zeebo/blake3"
)

// maxDirnameBytes is the longest directory name APFS, ext4, and NTFS
// allow. Grok Build URL-encodes the cwd into that name, and switches
// to a slug plus a BLAKE3 prefix when the encoding would be longer.
const maxDirnameBytes = 255

// EncodeCWDDirname is the group directory name Grok Build uses for cwd.
// A short cwd is percent-encoded. A longer one is slugify(leaf)- plus
// the first 16 hex characters of BLAKE3(cwd). The caller writes a .cwd
// file beside the session directories in that second case.
func EncodeCWDDirname(cwd string) string {
	encoded := PercentEncode(cwd)
	if len(encoded) <= maxDirnameBytes {
		return encoded
	}
	sum := blake3.Sum256([]byte(cwd))
	hash16 := hex.EncodeToString(sum[:8])
	slug := slugify(leafName(cwd), 40)
	if slug == "" {
		slug = "workspace"
	}
	return slug + "-" + hash16
}

// UsesCWDFile reports whether cwd's group directory is the slug-hash
// form. That directory holds a .cwd file with the original path.
func UsesCWDFile(cwd string) bool {
	return EncodeCWDDirname(cwd) != PercentEncode(cwd)
}

// PercentEncode percent-encodes every byte except ASCII alphanumerics
// and '-', '_', '.', '~'. The hex digits are uppercase, which is the
// encoding Grok Build uses for the group directory.
func PercentEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isUnreserved(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

// CWDFromGroup recovers a cwd from a sessions group directory. A
// URL-encoded absolute path decodes from the directory name. The
// slug-hash form does not, so the original path is the .cwd file in
// that directory.
func CWDFromGroup(dir string) (string, bool) {
	name := filepath.Base(dir)
	if decoded, ok := percentDecode(name); ok && absoluteCWD(decoded) {
		return decoded, true
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".cwd"))
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "", false
	}
	return s, true
}

func isUnreserved(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~'
}

func percentDecode(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		raw, err := hex.DecodeString(s[i+1 : i+3])
		if err != nil || len(raw) != 1 {
			return "", false
		}
		b.WriteByte(raw[0])
		i += 2
	}
	return b.String(), true
}

func absoluteCWD(s string) bool {
	if strings.HasPrefix(s, "/") {
		return true
	}
	if runtime.GOOS != "windows" || len(s) < 2 || s[1] != ':' {
		return false
	}
	c := s[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func leafName(cwd string) string {
	trimmed := strings.TrimRight(cwd, `/\`)
	if trimmed == "" {
		return "workspace"
	}
	i := strings.LastIndexAny(trimmed, `/\`)
	name := trimmed
	if i >= 0 {
		name = trimmed[i+1:]
	}
	if name == "" || name == "." || name == ".." {
		return "workspace"
	}
	return name
}

// slugify keeps ASCII letters and digits, folds other characters into
// a single dash, trims dashes, and keeps at most maxLen characters.
func slugify(input string, maxLen int) string {
	var b strings.Builder
	prevDash := false
	for _, c := range strings.ToLower(input) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	trimmed := strings.Trim(b.String(), "-")
	n := 0
	for i := range trimmed {
		if n == maxLen {
			return trimmed[:i]
		}
		n++
	}
	return trimmed
}
