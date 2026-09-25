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
	"sync"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// ProjectAt is the manifest project for a session cwd. CWDHash stays the
// path bucket the allowlist already matches. ProjectID is set only when
// origin and the root commit are both known. Those two are its only inputs.
//
// It reads files and does not run git. The checkout is not trusted
// before the allowlist admits it, and its own config can make git run
// a command. A root the in-process reader cannot find stays empty
// until ResolveRoot.
func ProjectAt(cwd string) protocol.Project {
	remote, head, root := "", "", ""
	if cwd != "" {
		remote, head, root = cachedCheckout(cwd)
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
	gitDir := findGitDir(cwd)
	if gitDir == "" {
		return "", ""
	}
	common := commonDir(gitDir)
	return originURL(filepath.Join(common, "config")), headCommit(gitDir, common)
}

// ResolveRoot fills GitRoot and ProjectID with git rev-list when the
// in-process reader left the root empty. It runs git, so call it only
// for a session the allowlist already admitted. A shallow checkout, or
// a HEAD git cannot walk, keeps the empty root.
func ResolveRoot(p protocol.Project) protocol.Project {
	if p.GitRoot != "" || p.CWD == "" || p.GitRemote == "" || !isHexCommit(p.GitCommit) {
		return p
	}
	gitDir := findGitDir(p.CWD)
	if gitDir == "" {
		return p
	}
	common := commonDir(gitDir)
	if shallowRepo(common) {
		return p
	}
	key := rootKey(common, p.GitCommit)
	root := cachedRoot(key)
	if root == "" && !recentMiss(key) {
		root = rootGit(gitDir, p.GitCommit)
		if root == "" {
			rememberMiss(key)
		}
	}
	if root == "" {
		return p
	}
	rememberRoot(key, root)
	p.GitRoot = root
	p.ProjectID = protocol.ProjectLinkID(p.GitRemote, root)
	return p
}

// OutsideCheckout reports whether cwd is a directory that no
// repository contains, so its empty remote is known rather than
// unreadable. A missing cwd, one inside a checkout, and one whose
// parents cannot be read are all false.
func OutsideCheckout(cwd string) bool {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return false
	}
	dir := filepath.Clean(cwd)
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
		dir = parent
	}
}

func readCheckout(gitDir string) (remote, head, root string) {
	common := commonDir(gitDir)
	remote = originURL(filepath.Join(common, "config"))
	head = headCommit(gitDir, common)
	if remote == "" || head == "" {
		return remote, head, ""
	}
	return remote, head, rootCommit(common, head)
}

// checkoutCache keeps what readCheckout read, per cwd, for the life of
// the process. An entry is used while every file it was read from
// still has the size and mtime it had then: HEAD, the ref HEAD names,
// packed-refs, config, commondir, and shallow. A commit moves the ref,
// a checkout moves HEAD, and a new origin moves config.
var checkoutCache = struct {
	sync.Mutex
	m map[string]checkout
}{m: map[string]checkout{}}

type checkout struct {
	gitDir             string
	files              []fileMark
	remote, head, root string
}

// fileMark is one input file's stat. A missing file is a mark too, so
// one that appears later is a change.
type fileMark struct {
	path    string
	exists  bool
	size    int64
	modTime time.Time
}

func markFile(path string) fileMark {
	st, err := os.Stat(path)
	if err != nil {
		return fileMark{path: path}
	}
	return fileMark{path: path, exists: true, size: st.Size(), modTime: st.ModTime()}
}

func (c checkout) fresh(gitDir string) bool {
	if c.gitDir != gitDir {
		return false
	}
	for _, f := range c.files {
		if m := markFile(f.path); m.exists != f.exists || m.size != f.size || !m.modTime.Equal(f.modTime) {
			return false
		}
	}
	return true
}

// checkoutFiles is every file readCheckout reads for gitDir. The ref is
// the one HEAD names now; a HEAD that moves to another ref changes
// HEAD's own mtime.
func checkoutFiles(gitDir string) []fileMark {
	common := commonDir(gitDir)
	paths := []string{
		filepath.Join(gitDir, "HEAD"),
		filepath.Join(gitDir, "commondir"),
		filepath.Join(common, "config"),
		filepath.Join(common, "packed-refs"),
		filepath.Join(common, "shallow"),
	}
	if b, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
		line := strings.TrimSpace(string(b))
		if ref, ok := strings.CutPrefix(line, "ref:"); ok {
			ref = filepath.FromSlash(strings.TrimSpace(ref))
			paths = append(paths, filepath.Join(gitDir, ref), filepath.Join(common, ref))
		}
	}
	out := make([]fileMark, len(paths))
	for i, p := range paths {
		out[i] = markFile(p)
	}
	return out
}

// cachedCheckout finds the repository that contains cwd and reads it
// through checkoutCache. The marks are taken before the read, so a
// write during it is a change next time.
func cachedCheckout(cwd string) (remote, head, root string) {
	gitDir := findGitDir(cwd)
	if gitDir == "" {
		return "", "", ""
	}
	checkoutCache.Lock()
	c, ok := checkoutCache.m[cwd]
	checkoutCache.Unlock()
	if ok && c.fresh(gitDir) {
		return c.remote, c.head, c.root
	}
	files := checkoutFiles(gitDir)
	remote, head, root = readCheckout(gitDir)
	checkoutCache.Lock()
	defer checkoutCache.Unlock()
	if len(checkoutCache.m) >= 4096 {
		checkoutCache.m = map[string]checkout{}
	}
	checkoutCache.m[cwd] = checkout{gitDir: gitDir, files: files, remote: remote, head: head, root: root}
	return remote, head, root
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

// originURL returns the URL of the remote named origin, without
// credentials. A repository that only has other remotes yields an
// empty string. A git_remote allow rule then fails closed. The
// operator can still allow the project by cwd prefix or cwd hash.
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
		return stripUserinfo(val)
	}
	return ""
}

// stripUserinfo drops credentials from a remote. An http or other URL
// loses its whole userinfo, since a token often sits in the user part
// alone. An ssh URL keeps a bare login name such as git and loses a
// password. The scp form is left as is: git ends its host at the first
// colon, so the part before an @ there is a login name, never a
// password.
func stripUserinfo(remote string) string {
	scheme, rest, ok := strings.Cut(remote, "://")
	if !ok {
		return remote
	}
	authority, _, _ := strings.Cut(rest, "/")
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return remote
	}
	user := authority[:at]
	switch strings.ToLower(scheme) {
	case "ssh", "git+ssh", "ssh+git":
		if !strings.Contains(user, ":") {
			return remote
		}
	}
	return scheme + "://" + rest[at+1:]
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
// stays empty rather than using the shallow boundary. Loose and packed
// objects are read in this process. A chain it cannot finish stays
// empty here, and ResolveRoot asks git once the allowlist has admitted
// the session.
func rootCommit(common, head string) string {
	if shallowRepo(common) {
		return ""
	}
	head = strings.ToLower(head)
	key := rootKey(common, head)
	if r := cachedRoot(key); r != "" {
		return r
	}
	r := rootWalk(common, head)
	if r != "" {
		rememberRoot(key, r)
	}
	return r
}

// rootCache keeps found roots. A commit's ancestry never changes, and
// every session in one checkout usually walks from the same HEAD.
var rootCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func rootKey(common, head string) string {
	return common + "\x00" + strings.ToLower(head)
}

func cachedRoot(key string) string {
	rootCache.Lock()
	defer rootCache.Unlock()
	return rootCache.m[key]
}

func rememberRoot(key, root string) {
	rootCache.Lock()
	defer rootCache.Unlock()
	if len(rootCache.m) >= 4096 {
		rootCache.m = map[string]string{}
	}
	rootCache.m[key] = root
}

// rootMisses is when git last failed to find a root, by repository
// and HEAD. git is not asked again for that pair until rootMissTTL has
// passed, so a checkout git cannot walk does not start a process for
// every session on every pass.
var rootMisses = struct {
	sync.Mutex
	m map[string]time.Time
}{m: map[string]time.Time{}}

const rootMissTTL = time.Hour

func recentMiss(key string) bool {
	rootMisses.Lock()
	defer rootMisses.Unlock()
	at, ok := rootMisses.m[key]
	return ok && time.Since(at) < rootMissTTL
}

func rememberMiss(key string) {
	rootMisses.Lock()
	defer rootMisses.Unlock()
	if len(rootMisses.m) >= 4096 {
		rootMisses.m = map[string]time.Time{}
	}
	rootMisses.m[key] = time.Now()
}

func shallowRepo(common string) bool {
	st, err := os.Stat(filepath.Join(common, "shallow"))
	return err == nil && st.Size() > 0
}

func rootWalk(common, head string) string {
	store := newObjectStore(common, len(head)/2)
	defer store.close()
	cur := head
	seen := map[string]struct{}{}
	for i := 0; i < 100000; i++ {
		if _, ok := seen[cur]; ok {
			return ""
		}
		seen[cur] = struct{}{}
		data, ok := store.commit(cur)
		if !ok {
			return ""
		}
		parent := firstParent(data)
		if parent == "" {
			return cur
		}
		if len(parent) != len(head) {
			return ""
		}
		cur = parent
	}
	return ""
}

// objectDirs is the repository's object directory and its alternates.
// A relative alternate is relative to the objects directory.
func objectDirs(common string) []string {
	objects := filepath.Join(common, "objects")
	dirs := []string{objects}
	b, err := os.ReadFile(filepath.Join(objects, "info", "alternates"))
	if err != nil {
		return dirs
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !filepath.IsAbs(line) {
			line = filepath.Join(objects, line)
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
	f, err := openRegular(path)
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

// gitHardening is the config every git call here pins on the command
// line, where it outranks the checkout's own .git/config. It turns off
// the settings that run a program: transports, hooks, fsmonitor, the
// ssh command, credential helpers, and automatic maintenance.
var gitHardening = []string{
	"-c", "protocol.allow=never",
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.sshCommand=false",
	"-c", "credential.helper=",
	"-c", "gc.auto=0",
	"-c", "maintenance.auto=false",
}

// gitEnv drops the parent environment. GIT_DIR would point git at the
// wrong repository, and a global or system config is not part of the
// checkout. GIT_ALLOW_PROTOCOL set to nothing overrides a per-protocol
// allow in the repository config, which protocol.allow does not.
// GIT_NO_LAZY_FETCH stops a partial clone from fetching a missing
// object; git older than 2.44 ignores it, and the protocol block holds.
var gitEnv = []string{
	"HOME=/",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_GLOBAL=/git-config-absent",
	"GIT_PAGER=cat",
	"GIT_ALLOW_PROTOCOL=",
	"GIT_PROTOCOL_FROM_USER=0",
	"GIT_NO_LAZY_FETCH=1",
	"GIT_NO_REPLACE_OBJECTS=1",
	"GIT_OPTIONAL_LOCKS=0",
}

// rootGit asks git for the root of head's first-parent chain in the
// repository at gitDir, the one this process already read.
func rootGit(gitDir, head string) string {
	if !isHexCommit(head) {
		return ""
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append([]string{"--git-dir=" + gitDir}, gitHardening...)
	args = append(args, "rev-list", "--max-parents=0", "--first-parent", head)
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, gitEnv...)
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
