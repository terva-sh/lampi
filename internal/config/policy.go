package config

import (
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
type ProjectMatch struct {
	CWDPrefix string `json:"cwd_prefix,omitempty"`
	GitRemote string `json:"git_remote,omitempty"`
	CWDHash   string `json:"cwd_hash,omitempty"`
}

// ProjectID is the project a session belongs to, taken from the harness
// meta line and, when the cwd still has a .git, from that repository.
type ProjectID struct {
	CWD       string
	CWDHash   string
	GitRemote string
}

// RedactionConfig is the explicit override for a ruleset hit.
// UploadHits defaults to false. Leave it false: a hit stays on the machine.
type RedactionConfig struct {
	UploadHits bool `json:"upload_hits,omitempty"`
}

// Permitted reports whether raw bytes for id may leave the machine.
func (p Projects) Permitted(id ProjectID) bool {
	if matchesAny(p.Deny, id) {
		return false
	}
	return matchesAny(p.Allow, id)
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
