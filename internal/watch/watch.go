// Package watch observes harness session files as they grow.
//
// The default layout is terva: append-only JSONL under $TERVA_HOME/sessions,
// including swarm and subagent files nested further down, and an optional
// *.errors.jsonl sidecar beside a transcript. Layout selects another
// harness tree, such as Claude Code's projects/ or Codex's sessions/.
// fsnotify is the preferred backend. A poll of the same walk is the
// fallback when fsnotify cannot be opened, and when ForcePoll is set
// (network filesystems, darwin, tests). A root that does not exist yet
// is polled until it does, and then fsnotify is tried. A watch that
// cannot be added, such as an inotify limit, or an error from fsnotify,
// such as a queue overflow, moves that watcher to polling for the rest
// of the run. OnFallback hears why, with the path. Run keeps going.
//
// Each path remembers size, mtime, and, on Unix, the inode. An append on
// a stable inode reports the previous size as Offset so the consumer
// reads the tail and not the prefix. A shrink, an inode change, or a
// remove followed by a new file at the same path reports Offset 0: the
// editor replaced or truncated the file, and the old cursor would skip
// bytes or read past the end.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/discover"
)

// Layout selects which files under Root are artifacts. The zero value
// watches Root/sessions the way terva lays files out, including
// *.errors.jsonl unless SkipErrors is set.
type Layout struct {
	// Dir is the directory under Root. Empty means sessions.
	Dir string
	// Match classifies a slash path relative to Root. Nil uses the
	// terva rule on the base name.
	Match func(rel string) (kind string, ok bool)
}

const (
	// OpCreate is a file that was not in the tree at the last observation.
	OpCreate = "create"
	// OpAppend is growth of a stable file. Offset is the previous size.
	OpAppend = "append"
	// OpReplace is a rewrite that is not a pure append: atomic rename,
	// same-length rewrite, or a shrink that grew back during debounce.
	OpReplace = "replace"
	// OpTruncate is a file that is shorter than the last observation.
	OpTruncate = "truncate"
)

// Change is one debounced observation. Offset is the first byte that was
// not already observed. Append leaves the prefix on disk unread.
type Change struct {
	AbsPath string
	RelPath string
	Kind    string
	Offset  int64
	Size    int64
	ModTime time.Time
	Op      string
}

// Watcher runs until ctx is cancelled.
type Watcher struct {
	// Root is a terva home. Files live in Root/sessions.
	Root string
	// OnChange is invoked from Run for each change. Nil discards the
	// change after the cursor moves. It must not call Run.
	OnChange func(Change)
	// Debounce is the quiet period for one path on the fsnotify backend.
	// Zero becomes 100ms. The poll backend coalesces by PollInterval.
	Debounce time.Duration
	// PollInterval is the walk period for the poll backend. Zero becomes 2s.
	PollInterval time.Duration
	// ForcePoll skips fsnotify.
	ForcePoll bool
	// SkipErrors leaves *.errors.jsonl untracked. The default is to track them.
	// It applies to the terva layout. Layout.Match is the authority when set.
	SkipErrors bool
	// Layout is the harness tree. The zero value is terva's sessions directory.
	Layout Layout
	// OnFallback is invoked from Run when this watcher moves to polling
	// after an fsnotify failure. The error names the path. Nil discards it.
	OnFallback func(error)

	ready   chan struct{}
	mu      sync.Mutex
	cursors map[string]*cursor
	mode    string
}

type cursor struct {
	rel   string
	kind  string
	size  int64
	mtime time.Time
	dev   uint64
	ino   uint64
	idOK  bool
}

type dirtyNote struct {
	reset    bool
	minSize  int64
	hasSize  bool
	deadline time.Time
}

// Probe reports which backend Run will prefer. It does not watch.
func Probe() string {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return "poll"
	}
	fsw.Close()
	return "fsnotify"
}

// PollByDefault reports whether the agent should set ForcePoll.
// LAMPI_WATCH=poll or LAMPI_WATCH=fsnotify decides. Unset polls on
// darwin, where kqueue holds one descriptor per watched file, and
// prefers fsnotify elsewhere.
func PollByDefault(getenv func(string) string) (bool, error) {
	switch v := getenv("LAMPI_WATCH"); v {
	case "poll":
		return true, nil
	case "fsnotify":
		return false, nil
	case "":
		return runtime.GOOS == "darwin", nil
	default:
		return false, fmt.Errorf("watch: LAMPI_WATCH is %q; use poll or fsnotify", v)
	}
}

// Backend is what Run will use with ForcePoll set to poll.
func Backend(poll bool) string {
	if poll {
		return "poll"
	}
	return Probe()
}

// ensure returns the ready channel. The map and the channel are created
// under w.mu so Tracked can run before Run finishes seeding.
func (w *Watcher) ensure() chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cursors == nil {
		w.cursors = map[string]*cursor{}
	}
	if w.ready == nil {
		w.ready = make(chan struct{})
	}
	return w.ready
}

// Mode is "fsnotify" or "poll" after Run has started its backend.
// It is empty before that.
func (w *Watcher) Mode() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mode
}

// WaitReady returns when the first directory walk has seeded cursors.
// It returns ctx.Err() if ctx ends first. Run closes the ready channel
// only after a successful seed, so a failed Run does not unblock this
// with a nil error. Cancel ctx when Run has failed.
func (w *Watcher) WaitReady(ctx context.Context) error {
	ready := w.ensure()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

// Tracked lists relative paths currently cursor'd, in slash form, sorted.
func (w *Watcher) Tracked() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.cursors) == 0 {
		return nil
	}
	out := make([]string, 0, len(w.cursors))
	for _, c := range w.cursors {
		out = append(out, c.rel)
	}
	sort.Strings(out)
	return out
}

// Run watches until ctx is cancelled. A normal stop returns nil.
func (w *Watcher) Run(ctx context.Context) error {
	ready := w.ensure()

	if w.Root == "" {
		return fmt.Errorf("watch: root is empty")
	}
	debounce := w.Debounce
	if debounce <= 0 {
		debounce = 100 * time.Millisecond
	}
	pollEvery := w.PollInterval
	if pollEvery <= 0 {
		pollEvery = 2 * time.Second
	}

	var fsw *fsnotify.Watcher
	// retry is a root that is not there yet. Polling finds it, and
	// then fsnotify is tried again.
	retry := false
	if !w.ForcePoll {
		var err error
		fsw, err = w.openFS()
		switch {
		case errors.Is(err, errNoRoot):
			retry = true
		case err != nil:
			w.fallback(err)
		}
	}
	if fsw != nil {
		w.setMode("fsnotify")
	} else {
		w.setMode("poll")
	}

	if err := w.seed(); err != nil {
		if fsw != nil {
			fsw.Close()
		}
		return err
	}
	close(ready)

	if err := ctx.Err(); err != nil {
		if fsw != nil {
			fsw.Close()
		}
		return nil
	}
	for {
		if fsw != nil {
			err := w.loopFS(ctx, fsw, debounce)
			if err == nil || ctx.Err() != nil {
				return nil
			}
			// Events may have been lost. Poll from here on, and diff at
			// once so a change fsnotify dropped is not left waiting.
			w.fallback(err)
			w.setMode("poll")
			fsw = nil
			retry = false
			changes, err := w.diff()
			if err != nil {
				return err
			}
			w.emit(changes)
		}
		next, err := w.loopPoll(ctx, pollEvery, retry)
		if err != nil || next == nil {
			return err
		}
		fsw = next
		w.setMode("fsnotify")
	}
}

// errNoRoot is a Root that does not exist yet.
var errNoRoot = errors.New("watch: root does not exist")

// openFS starts fsnotify on Root and the layout tree. A missing Root is
// errNoRoot. Any other failure names the path it was adding.
func (w *Watcher) openFS() (*fsnotify.Watcher, error) {
	if _, err := os.Stat(w.Root); errors.Is(err, fs.ErrNotExist) {
		return nil, errNoRoot
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: fsnotify: %w", err)
	}
	// Watch before the seed so a create during the walk is queued
	// and reconciled against the cursor instead of lost.
	if err := addWatch(fsw, w.Root); err != nil {
		fsw.Close()
		return nil, fmt.Errorf("watch: %s: %w", w.Root, err)
	}
	if err := addTree(fsw, filepath.Join(w.Root, w.subdir())); err != nil {
		fsw.Close()
		return nil, err
	}
	if testOpened != nil {
		testOpened(fsw)
	}
	return fsw, nil
}

// addWatch is fsw.Add. A test replaces it to fail the way an inotify
// watch limit does.
var addWatch = func(fsw *fsnotify.Watcher, path string) error { return fsw.Add(path) }

// testOpened sees each fsnotify watcher openFS starts. It is nil
// outside tests.
var testOpened func(*fsnotify.Watcher)

func (w *Watcher) fallback(err error) {
	if w.OnFallback != nil {
		w.OnFallback(fmt.Errorf("%w; polling %s", err, filepath.Join(w.Root, w.subdir())))
	}
}

func (w *Watcher) setMode(mode string) {
	w.mu.Lock()
	w.mode = mode
	w.mu.Unlock()
}

func (w *Watcher) subdir() string {
	if w.Layout.Dir != "" {
		return w.Layout.Dir
	}
	return "sessions"
}

func (w *Watcher) list() ([]discover.File, error) {
	if w.Layout.Match == nil {
		return discover.Sessions(w.Root)
	}
	refs, err := adapter.Walk(w.Root, w.subdir(), w.Layout.Match)
	if err != nil {
		return nil, err
	}
	out := make([]discover.File, len(refs))
	for i, r := range refs {
		out[i] = discover.File{
			AbsPath: r.AbsPath,
			RelPath: r.RelPath,
			Size:    r.Size,
			ModTime: r.ModTime,
			Kind:    r.Kind,
		}
	}
	return out, nil
}

// classifyPath reports whether path is an artifact. rel is slash-separated
// from Root. A path outside the layout directory is not.
func (w *Watcher) classifyPath(path string) (rel, kind string, ok bool) {
	if !inTree(w.Root, w.subdir(), path) {
		return "", "", false
	}
	rel, err := relPath(w.Root, path)
	if err != nil {
		return "", "", false
	}
	if w.Layout.Match != nil {
		kind, ok = w.Layout.Match(rel)
		return rel, kind, ok
	}
	kind, ok = classify(filepath.Base(path), w.SkipErrors)
	return rel, kind, ok
}

func (w *Watcher) seed() error {
	files, err := w.list()
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, f := range files {
		if w.SkipErrors && f.Kind == discover.KindErrors {
			continue
		}
		info, err := os.Stat(f.AbsPath)
		if err != nil {
			continue
		}
		w.cursors[f.AbsPath] = cursorFrom(f.RelPath, f.Kind, info)
	}
	return nil
}

// loopPoll diffs every tick. With retry set, a tick that finds Root
// tries fsnotify, and on success returns that watcher after one diff so
// nothing written in between is missed. A nil watcher is a stop.
func (w *Watcher) loopPoll(ctx context.Context, every time.Duration, retry bool) (*fsnotify.Watcher, error) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-ticker.C:
			var fsw *fsnotify.Watcher
			if retry {
				var err error
				fsw, err = w.openFS()
				if err != nil && !errors.Is(err, errNoRoot) {
					retry = false
					w.fallback(err)
				}
			}
			changes, err := w.diff()
			if err != nil {
				if fsw != nil {
					fsw.Close()
				}
				return nil, err
			}
			w.emit(changes)
			if fsw != nil {
				return fsw, nil
			}
		}
	}
}

func (w *Watcher) loopFS(ctx context.Context, fsw *fsnotify.Watcher, debounce time.Duration) error {
	defer fsw.Close()
	dirty := map[string]*dirtyNote{}
	// Each event pushes that path's deadline forward. The tick flushes a
	// path once the quiet period has elapsed.
	tickEvery := 10 * time.Millisecond
	if debounce < tickEvery {
		tickEvery = debounce
	}
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-fsw.Errors:
			if !ok {
				return fmt.Errorf("watch: %s: fsnotify closed", w.Root)
			}
			if err != nil {
				return fmt.Errorf("watch: %s: %w", w.Root, err)
			}
		case ev, ok := <-fsw.Events:
			if !ok {
				return fmt.Errorf("watch: %s: fsnotify closed", w.Root)
			}
			if err := w.noteEvent(fsw, ev, dirty, debounce); err != nil {
				return err
			}
		case now := <-ticker.C:
			w.emit(w.flush(dirty, now))
		}
	}
}

func (w *Watcher) noteEvent(fsw *fsnotify.Watcher, ev fsnotify.Event, dirty map[string]*dirtyNote, debounce time.Duration) error {
	if !ev.Has(fsnotify.Create) && !ev.Has(fsnotify.Write) && !ev.Has(fsnotify.Remove) && !ev.Has(fsnotify.Rename) {
		return nil
	}
	path := ev.Name
	if !inTree(w.Root, w.subdir(), path) {
		return nil
	}
	info, statErr := os.Lstat(path)
	if statErr == nil && info.IsDir() {
		if ev.Has(fsnotify.Create) {
			// A new directory is not watched until we add it. Files
			// created inside it before Add are invisible to fsnotify,
			// so the walk is what discovers swarm and subagent files.
			if err := addTree(fsw, path); err != nil {
				return err
			}
			return w.noteTree(path, dirty, debounce)
		}
		return nil
	}
	if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
		markReset(dirty, path, debounce)
		if statErr != nil {
			return nil
		}
	}
	if statErr != nil {
		return nil
	}
	if _, _, ok := w.classifyPath(path); !ok {
		return nil
	}
	touchDirty(dirty, path, info.Size(), false, debounce)
	return nil
}

func (w *Watcher) flush(dirty map[string]*dirtyNote, now time.Time) []Change {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []Change
	for path, n := range dirty {
		if n.deadline.After(now) {
			continue
		}
		delete(dirty, path)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				delete(w.cursors, path)
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		rel, kind, ok := w.classifyPath(path)
		if !ok {
			delete(w.cursors, path)
			continue
		}
		reset := n.reset
		if cur, exists := w.cursors[path]; exists && n.hasSize && n.minSize < cur.size {
			reset = true
		}
		if ch, ok := w.observeLocked(path, rel, kind, info, reset); ok {
			out = append(out, ch)
		}
	}
	return out
}

func (w *Watcher) diff() ([]Change, error) {
	files, err := w.list()
	if err != nil {
		return nil, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	seen := map[string]struct{}{}
	var out []Change
	for _, f := range files {
		if w.SkipErrors && f.Kind == discover.KindErrors {
			continue
		}
		seen[f.AbsPath] = struct{}{}
		info, err := os.Stat(f.AbsPath)
		if err != nil {
			if os.IsNotExist(err) {
				delete(w.cursors, f.AbsPath)
			}
			continue
		}
		if ch, ok := w.observeLocked(f.AbsPath, f.RelPath, f.Kind, info, false); ok {
			out = append(out, ch)
		}
	}
	for path := range w.cursors {
		if _, ok := seen[path]; !ok {
			delete(w.cursors, path)
		}
	}
	return out, nil
}

func (w *Watcher) emit(changes []Change) {
	if w.OnChange == nil {
		return
	}
	for _, c := range changes {
		w.OnChange(c)
	}
}

// observeLocked updates the cursor. The caller holds w.mu.
// It does not read file bytes. Offset is the previous size on append.
func (w *Watcher) observeLocked(abs, rel, kind string, info os.FileInfo, reset bool) (Change, bool) {
	dev, ino, idOK := identity(info)
	size := info.Size()
	mtime := info.ModTime()
	cur, exists := w.cursors[abs]
	if !exists {
		w.cursors[abs] = &cursor{rel: rel, kind: kind, size: size, mtime: mtime, dev: dev, ino: ino, idOK: idOK}
		return Change{
			AbsPath: abs, RelPath: rel, Kind: kind,
			Offset: 0, Size: size, ModTime: mtime.UTC(), Op: OpCreate,
		}, true
	}
	replaced := reset || (cur.idOK && idOK && (cur.dev != dev || cur.ino != ino))
	var op string
	offset := int64(0)
	switch {
	case replaced && size < cur.size:
		op = OpTruncate
	case replaced:
		op = OpReplace
	case size < cur.size:
		op = OpTruncate
	case size > cur.size:
		op = OpAppend
		offset = cur.size
	case !mtime.Equal(cur.mtime):
		op = OpReplace
	default:
		return Change{}, false
	}
	cur.rel = rel
	cur.kind = kind
	cur.size = size
	cur.mtime = mtime
	cur.dev, cur.ino, cur.idOK = dev, ino, idOK
	return Change{
		AbsPath: abs, RelPath: rel, Kind: kind,
		Offset: offset, Size: size, ModTime: mtime.UTC(), Op: op,
	}, true
}

func cursorFrom(rel, kind string, info os.FileInfo) *cursor {
	dev, ino, ok := identity(info)
	return &cursor{
		rel: rel, kind: kind, size: info.Size(), mtime: info.ModTime(),
		dev: dev, ino: ino, idOK: ok,
	}
}

func classify(name string, skipErrors bool) (string, bool) {
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".jsonl") {
		return "", false
	}
	if strings.HasSuffix(name, ".errors.jsonl") {
		if skipErrors {
			return "", false
		}
		return discover.KindErrors, true
	}
	return discover.KindTranscript, true
}

func inTree(root, dir, path string) bool {
	base := filepath.Clean(filepath.Join(root, dir))
	path = filepath.Clean(path)
	if path == base {
		return true
	}
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func relPath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// addTree watches root and every directory under it. A symlinked root
// is followed. A directory that vanished or cannot be read is not
// watched; the poll fallback would not see into it either. An Add that
// fails, such as the inotify watch limit, names the path.
func addTree(fsw *fsnotify.Watcher, root string) error {
	info, err := os.Stat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	if err := addWatch(fsw, root); err != nil {
		return fmt.Errorf("watch: %s: %w", root, err)
	}
	_, err = discover.WalkDirs(root, func(path string) error {
		if err := addWatch(fsw, path); err != nil {
			if discover.Skippable(err) {
				return err
			}
			return fmt.Errorf("watch: %s: %w", path, err)
		}
		return nil
	})
	return err
}

func (w *Watcher) noteTree(dir string, dirty map[string]*dirtyNote, debounce time.Duration) error {
	_, err := discover.WalkFiles(dir, func(path string, d fs.DirEntry) error {
		if _, _, ok := w.classifyPath(path); !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		touchDirty(dirty, path, info.Size(), false, debounce)
		return nil
	})
	return err
}

func touchDirty(dirty map[string]*dirtyNote, path string, size int64, reset bool, debounce time.Duration) {
	n := dirty[path]
	if n == nil {
		n = &dirtyNote{}
		dirty[path] = n
	}
	if reset {
		n.reset = true
	}
	if !n.hasSize || size < n.minSize {
		n.minSize = size
		n.hasSize = true
	}
	n.deadline = time.Now().Add(debounce)
}

func markReset(dirty map[string]*dirtyNote, path string, debounce time.Duration) {
	n := dirty[path]
	if n == nil {
		n = &dirtyNote{}
		dirty[path] = n
	}
	n.reset = true
	n.deadline = time.Now().Add(debounce)
}
