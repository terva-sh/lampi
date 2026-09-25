package upload

import (
	"sync"
	"time"

	"terva.sh/lampi/internal/adapter"
)

// FullPassEvery is how long a Memo trusts a file's size, mtime, and
// inode. After that one pass reads and hashes every file again, the
// way the first pass of a process does.
const FullPassEvery = 6 * time.Hour

// Memo is what the readers took from each file, kept across passes by
// a long-running caller. A file whose size, mtime, and inode still
// match is not opened: its digest and session come from the memo, and
// a session whose digests all match their watermarks is not read,
// scanned, or posted.
//
// A file rewritten in place with the same size, mtime, and inode is
// taken as unchanged until the next full pass. The first pass after
// NewMemo, and one every FullPassEvery, hashes every file, so such a
// rewrite waits at most that long. A nil Memo hashes every file on
// every pass; a session whose digests match still is not posted.
type Memo struct {
	mu       sync.Mutex
	files    map[string]memoEntry
	lastFull time.Time
	full     bool
	// ran is the harnesses this pass walked, and touched the files it
	// saw. end forgets a file of a harness that ran and did not see it.
	ran     map[string]bool
	touched map[string]bool
}

type memoEntry struct {
	harness string
	stat    adapter.FileStat
	seen    adapter.Seen
}

// NewMemo is an empty memo. Its first pass is a full pass.
func NewMemo() *Memo {
	return &Memo{files: map[string]memoEntry{}}
}

// begin starts a pass at now and reports whether it is a full pass.
func (m *Memo) begin(now time.Time) bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.full = m.lastFull.IsZero() || now.Sub(m.lastFull) >= FullPassEvery || now.Before(m.lastFull)
	if m.full {
		m.lastFull = now
	}
	m.ran = map[string]bool{}
	m.touched = map[string]bool{}
	return m.full
}

// end forgets the files a harness that ran this pass no longer has.
func (m *Memo) end() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, e := range m.files {
		if m.ran[e.harness] && !m.touched[key] {
			delete(m.files, key)
		}
	}
	m.ran, m.touched = nil, nil
}

// forHarness is the adapter.Memo one harness reads through. It is nil
// for a nil Memo, so the reader hashes every file.
func (m *Memo) forHarness(harness string) adapter.Memo {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ran != nil {
		m.ran[harness] = true
	}
	return harnessMemo{m: m, harness: harness}
}

type harnessMemo struct {
	m       *Memo
	harness string
}

func (h harnessMemo) key(path string) string { return h.harness + "\x00" + path }

// Recall is a miss on a full pass, so every file is read and hashed.
func (h harnessMemo) Recall(path string, st adapter.FileStat) (adapter.Seen, bool) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	key := h.key(path)
	if h.m.touched != nil {
		h.m.touched[key] = true
	}
	e, ok := h.m.files[key]
	if h.m.full || !ok || e.stat.Size != st.Size || e.stat.Inode != st.Inode || !e.stat.ModTime.Equal(st.ModTime) {
		return adapter.Seen{}, false
	}
	return e.seen, true
}

func (h harnessMemo) Remember(path string, st adapter.FileStat, s adapter.Seen) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	key := h.key(path)
	if h.m.touched != nil {
		h.m.touched[key] = true
	}
	h.m.files[key] = memoEntry{harness: h.harness, stat: st, seen: s}
}
