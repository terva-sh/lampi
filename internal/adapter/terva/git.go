package terva

import (
	"os"
	"path/filepath"
	"strings"
)

// projectGit reads origin's URL and HEAD from the repository that contains
// cwd. A missing directory, a missing .git, or an unreadable config is
// an empty result: the allowlist can still match the cwd itself.
func projectGit(cwd string) (remote, commit string) {
	if cwd == "" {
		return "", ""
	}
	gitDir := findGitDir(cwd)
	if gitDir == "" {
		return "", ""
	}
	common := commonDir(gitDir)
	return originURL(filepath.Join(common, "config")), headCommit(gitDir, common)
}

func findGitDir(start string) string {
	dir := start
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return ""
	}
	for {
		candidate := filepath.Join(dir, ".git")
		st, err := os.Stat(candidate)
		if err == nil {
			if st.IsDir() {
				return candidate
			}
			return gitdirFile(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitdirFile reads a .git file, the form worktrees use. The gitdir path
// may be absolute or relative to the file's directory.
func gitdirFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if gitdir == "" {
		return ""
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(filepath.Dir(path), gitdir)
	}
	return filepath.Clean(gitdir)
}

// commonDir is the main .git directory. A worktree git dir points at it
// with a commondir file, which is where config lives.
func commonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	line := strings.TrimSpace(string(b))
	if line == "" {
		return gitDir
	}
	if filepath.IsAbs(line) {
		return filepath.Clean(line)
	}
	return filepath.Clean(filepath.Join(gitDir, line))
}

// originURL returns the URL of the remote named origin. A repository
// that only has other remotes yields an empty string. A git_remote
// allow rule then fails closed. The operator can still allow the
// project by cwd prefix or cwd hash.
func originURL(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	section := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.ToLower(line)
			continue
		}
		key, val, ok := splitINI(line)
		if !ok || key != "url" || !strings.HasPrefix(section, "[remote ") || !strings.Contains(section, `"origin"`) {
			continue
		}
		return val
	}
	return ""
}

func splitINI(line string) (key, val string, ok bool) {
	key, val, ok = strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(key))
	val = strings.TrimSpace(val)
	val = strings.Trim(val, `"`)
	if key == "" || val == "" {
		return "", "", false
	}
	return key, val, true
}

func headCommit(gitDir, common string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	if strings.HasPrefix(line, "ref:") {
		ref := strings.TrimSpace(strings.TrimPrefix(line, "ref:"))
		for _, base := range []string{gitDir, common} {
			raw, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(ref)))
			if err != nil {
				continue
			}
			s := strings.TrimSpace(string(raw))
			if isHexCommit(s) {
				return s
			}
		}
		return packedRef(common, ref)
	}
	if isHexCommit(line) {
		return line
	}
	return ""
}

func packedRef(common, ref string) string {
	b, err := os.ReadFile(filepath.Join(common, "packed-refs"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue
		}
		sum, name, ok := strings.Cut(line, " ")
		if !ok || name != ref {
			continue
		}
		sum = strings.TrimSpace(sum)
		if isHexCommit(sum) {
			return sum
		}
	}
	return ""
}

func isHexCommit(s string) bool {
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
