package adapter

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// ProjectAt is the manifest project for a session cwd. CWDHash stays the
// path bucket the allowlist already matches. ProjectID is set only when
// origin and the root commit are both known. Those two are its only inputs.
func ProjectAt(cwd string) protocol.Project {
	remote, head, root := "", "", ""
	if cwd != "" {
		remote, head, root = projectCheckout(cwd)
	}
	return protocol.Project{
		CWD:       cwd,
		CWDHash:   CWDHash(cwd),
		GitRemote: remote,
		GitCommit: head,
		GitRoot:   root,
		ProjectID: protocol.ProjectLinkID(remote, root),
	}
}

// ProjectGit reads origin's URL and HEAD from the repository that contains
// cwd. A missing directory, a missing .git, or an unreadable config is
// an empty result: the allowlist can still match the cwd itself.
func ProjectGit(cwd string) (remote, commit string) {
	if cwd == "" {
		return "", ""
	}
	remote, commit, _ = projectCheckout(cwd)
	return remote, commit
}

func projectCheckout(cwd string) (remote, head, root string) {
	gitDir := findGitDir(cwd)
	if gitDir == "" {
		return "", "", ""
	}
	common := commonDir(gitDir)
	remote = originURL(filepath.Join(common, "config"))
	head = headCommit(gitDir, common)
	if remote == "" || head == "" {
		return remote, head, ""
	}
	return remote, head, rootCommit(cwd, common, head)
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

// rootCommit is the first parentless commit on HEAD's first-parent
// chain. A shallow repository does not reveal that commit, so the id
// stays empty rather than using the shallow boundary. Loose objects are
// read directly. When that chain is packed or stored in an alternate
// this process cannot see, git rev-list is the fallback.
func rootCommit(cwd, common, head string) string {
	if shallowRepo(common) {
		return ""
	}
	if r := rootLoose(common, head); r != "" {
		return r
	}
	return rootGit(cwd)
}

func shallowRepo(common string) bool {
	st, err := os.Stat(filepath.Join(common, "shallow"))
	return err == nil && st.Size() > 0
}

func rootLoose(common, head string) string {
	dirs := objectDirs(common)
	cur := strings.ToLower(head)
	seen := map[string]struct{}{}
	for i := 0; i < 100000; i++ {
		if _, ok := seen[cur]; ok {
			return ""
		}
		seen[cur] = struct{}{}
		data, ok := looseCommit(dirs, cur)
		if !ok {
			return ""
		}
		parent := firstParent(data)
		if parent == "" {
			return cur
		}
		cur = parent
	}
	return ""
}

func objectDirs(common string) []string {
	dirs := []string{filepath.Join(common, "objects")}
	b, err := os.ReadFile(filepath.Join(common, "objects", "info", "alternates"))
	if err != nil {
		return dirs
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(common, line)
		}
		dirs = append(dirs, filepath.Clean(line))
	}
	return dirs
}

func looseCommit(dirs []string, hash string) ([]byte, bool) {
	if !isHexCommit(hash) {
		return nil, false
	}
	var raw []byte
	var found bool
	for _, dir := range dirs {
		b, ok := readLoose(filepath.Join(dir, hash[:2], hash[2:]))
		if !ok {
			continue
		}
		raw = b
		found = true
		break
	}
	if !found {
		return nil, false
	}
	typ, data, ok := splitGitObject(raw)
	if !ok || typ != "commit" {
		return nil, false
	}
	return data, true
}

func readLoose(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	zr, err := zlib.NewReader(f)
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	b, err := io.ReadAll(io.LimitReader(zr, 1<<20))
	if err != nil {
		return nil, false
	}
	return b, true
}

func splitGitObject(raw []byte) (typ string, data []byte, ok bool) {
	head, rest, found := bytes.Cut(raw, []byte{0})
	if !found {
		return "", nil, false
	}
	kind, _, found := strings.Cut(string(head), " ")
	if !found || kind == "" {
		return "", nil, false
	}
	return kind, rest, true
}

func firstParent(data []byte) string {
	for len(data) > 0 {
		line, rest, _ := bytes.Cut(data, []byte("\n"))
		data = rest
		if len(line) == 0 {
			return ""
		}
		const prefix = "parent "
		if !bytes.HasPrefix(line, []byte(prefix)) {
			continue
		}
		parent := strings.TrimSpace(string(line[len(prefix):]))
		if isHexCommit(parent) {
			return parent
		}
		return ""
	}
	return ""
}

func rootGit(cwd string) string {
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, "-C", cwd, "rev-list", "--max-parents=0", "--first-parent", "HEAD")
	// Drop the parent environment. GIT_DIR would point this process at
	// the wrong repository, and a global config is not part of the checkout.
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=/",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/git-config-absent",
		"GIT_PAGER=cat",
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return ""
	}
	line := strings.TrimSpace(out.String())
	line, _, _ = strings.Cut(line, "\n")
	line = strings.ToLower(strings.TrimSpace(line))
	if !isHexCommit(line) {
		return ""
	}
	return line
}
