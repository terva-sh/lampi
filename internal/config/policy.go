package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/protocol"
)

// Projects is the allow and deny lists for off-box raw.
// Deny wins. A project that matches no allow rule is refused, including
// when Allow is empty. That empty list is the default.
type Projects struct {
	Allow []ProjectMatch `json:"allow,omitempty"`
	Deny  []ProjectMatch `json:"deny,omitempty"`
}

// ProjectMatch is one rule. Every field that is set must match. A rule
// with no fields matches nothing, so an empty object does not allow the
// world. CWDPrefix is matched on a path boundary. GitRemote is compared
// after NormalizeRemote, so the scp and https spellings of one remote
// are the same rule. CWDHash is terva's hex(sha256(cwd)[:8]).
//
// A deny rule reads a doubt as a match. Its cwd_prefix ignores case,
// and it is tried against the cwd and the prefix as written and with
// symlinks resolved. Its cwd_hash also matches the hash of the
// resolved cwd. Its git_remote also matches a
// session whose remote is unknown. An allow rule compares exactly, so
// a case or symlink variant of an allowed path is refused.
type ProjectMatch struct {
	CWDPrefix string `json:"cwd_prefix,omitempty"`
	GitRemote string `json:"git_remote,omitempty"`
	CWDHash   string `json:"cwd_hash,omitempty"`
}

// ProjectID is the project a session belongs to, taken from the harness
// meta line and, when the cwd still has a .git, from that repository.
//
// NoRepo is true when the caller checked that the cwd exists and that
// no repository contains it, so an empty GitRemote means the session
// has no remote. The zero value means an empty remote is unknown: the
// cwd is gone, or the checkout has no readable origin.
type ProjectID struct {
	CWD       string
	CWDHash   string
	GitRemote string
	NoRepo    bool
}

// RedactionConfig is the explicit override for a ruleset hit.
// UploadHits defaults to false. Leave it false: a hit stays on the machine.
type RedactionConfig struct {
	UploadHits bool `json:"upload_hits,omitempty"`
}

// Permitted reports whether raw bytes for id may leave the machine.
func (p Projects) Permitted(id ProjectID) bool {
	if deniedByAny(p.Deny, id) {
		return false
	}
	return matchesAny(p.Allow, id)
}

func deniedByAny(rules []ProjectMatch, id ProjectID) bool {
	if len(rules) == 0 {
		return false
	}
	cwds := withResolved(id.CWD)
	for _, r := range rules {
		for _, cwd := range cwds {
			if r.denies(id, cwd) {
				return true
			}
		}
	}
	return false
}

// denies is matches read the way ProjectMatch describes for a deny
// rule. cwd is the session cwd or its resolved form.
func (r ProjectMatch) denies(id ProjectID, cwd string) bool {
	if r.CWDPrefix == "" && r.GitRemote == "" && r.CWDHash == "" {
		return false
	}
	if r.CWDPrefix != "" && !denyPrefix(cwd, r.CWDPrefix) {
		return false
	}
	if r.CWDHash != "" {
		want := strings.TrimSpace(r.CWDHash)
		if !strings.EqualFold(want, strings.TrimSpace(id.CWDHash)) && !strings.EqualFold(want, cwdHash(cwd)) {
			return false
		}
	}
	if r.GitRemote != "" {
		unknown := id.GitRemote == "" && !id.NoRepo
		if !unknown && NormalizeRemote(r.GitRemote) != NormalizeRemote(id.GitRemote) {
			return false
		}
	}
	return true
}

// denyPrefix folds case, since APFS, NTFS, and some Linux mounts treat
// a case variant as the same directory. The prefix is tried as written
// and with its symlinks resolved.
func denyPrefix(cwd, prefix string) bool {
	for _, p := range withResolved(prefix) {
		if cwdHasPrefix(strings.ToLower(cwd), strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// withResolved is path, plus its EvalSymlinks form when that resolves
// to something else. A path that does not resolve, or is not absolute,
// is used as written.
func withResolved(path string) []string {
	out := []string{path}
	if !filepath.IsAbs(path) {
		return out
	}
	if r, err := filepath.EvalSymlinks(path); err == nil && r != filepath.Clean(path) {
		out = append(out, r)
	}
	return out
}

// cwdHash is adapter.CWDHash: hex(sha256(cwd)[:8]).
func cwdHash(cwd string) string {
	if cwd == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(cwd))
	return hex.EncodeToString(sum[:8])
}

func matchesAny(rules []ProjectMatch, id ProjectID) bool {
	for _, r := range rules {
		if r.matches(id) {
			return true
		}
	}
	return false
}

func (r ProjectMatch) matches(id ProjectID) bool {
	if r.CWDPrefix == "" && r.GitRemote == "" && r.CWDHash == "" {
		return false
	}
	if r.CWDPrefix != "" && !cwdHasPrefix(id.CWD, r.CWDPrefix) {
		return false
	}
	if r.CWDHash != "" && !strings.EqualFold(strings.TrimSpace(r.CWDHash), strings.TrimSpace(id.CWDHash)) {
		return false
	}
	if r.GitRemote != "" && NormalizeRemote(r.GitRemote) != NormalizeRemote(id.GitRemote) {
		return false
	}
	return true
}

// cwdHasPrefix reports whether cwd is prefix or a child of prefix.
// "/tmp/proj" matches "/tmp/proj" and "/tmp/proj/sub". It does not match
// "/tmp/proj-other". An empty prefix matches nothing.
func cwdHasPrefix(cwd, prefix string) bool {
	if cwd == "" || prefix == "" {
		return false
	}
	cwd = filepath.Clean(cwd)
	prefix = filepath.Clean(prefix)
	if prefix == "." {
		return false
	}
	if cwd == prefix {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(prefix, sep) {
		prefix += sep
	}
	return strings.HasPrefix(cwd, prefix)
}

// NormalizeRemote folds the spellings of one git remote into one key.
// The allowlist and protocol.ProjectLinkID share this function, so
// "git@github.com:org/repo.git" and "https://github.com/org/repo" are
// the same remote in a rule and in a project id.
func NormalizeRemote(s string) string {
	return protocol.NormalizeRemote(s)
}
