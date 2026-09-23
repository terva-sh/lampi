package protocol

import (
	"net"
	"net/url"
	"strings"
)

// NormalizeRemote folds the spellings of one git remote into one key.
// "git@github.com:org/repo.git" and "https://github.com/org/repo" both
// become "github.com/org/repo". The comparison is case-insensitive.
// An empty string stays empty, and does not match a real remote.
func NormalizeRemote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	if strings.HasPrefix(s, "git@") && strings.Contains(s, ":") && !strings.Contains(s, "://") {
		rest := strings.TrimPrefix(s, "git@")
		host, path, ok := strings.Cut(rest, ":")
		if ok {
			return strings.ToLower(host + "/" + strings.TrimPrefix(path, "/"))
		}
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err == nil && u.Host != "" {
			host := u.Hostname()
			if host == "" {
				host = u.Host
				if h, _, err := net.SplitHostPort(host); err == nil {
					host = h
				}
			}
			path := strings.TrimPrefix(u.Path, "/")
			path = strings.TrimSuffix(path, "/")
			return strings.ToLower(host + "/" + path)
		}
	}
	return strings.ToLower(strings.TrimPrefix(s, "/"))
}

// ProjectLinkID is the Layer C project key. It is the normalized origin
// URL, an "@", and the lowercase root commit. The cwd and cwd_hash are
// not inputs. An empty remote, or a root that is not 40 or 64 lowercase
// hex characters after trimming, returns an empty string. Empty ids are
// not a group: two checkouts without a root do not link.
func ProjectLinkID(remote, root string) string {
	remote = NormalizeRemote(remote)
	root = strings.ToLower(strings.TrimSpace(root))
	if remote == "" || !commitHex(root) {
		return ""
	}
	return remote + "@" + root
}

func commitHex(s string) bool {
	if len(s) != 40 && len(s) != 64 {
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
