package adapter

import (
	"encoding/json"
	"time"
)

// FileStat is what a reader compares before it opens a file again.
// Inode is zero where the platform has none.
type FileStat struct {
	Size    int64
	ModTime time.Time
	Inode   uint64
}

// Seen is what a reader took from one file: the sha256 of its bytes
// and the identity it parsed, in the reader's own encoding.
type Seen struct {
	SHA256 string
	Ident  []byte
}

// Memo remembers Seen by path and stat. Recall is ok only when st is
// the stat the entry was remembered at. A file rewritten in place with
// the same size, mtime, and inode is recalled as it was; the memo's
// owner decides how often to stop trusting the stat and read again.
type Memo interface {
	Recall(path string, st FileStat) (Seen, bool)
	Remember(path string, st FileStat, s Seen)
}

// Identify returns the digest of the file ref names and the identity
// read parses from it. When memo recalls ref at the stat the walk saw,
// the file is not opened and read is not called. Otherwise both run,
// and the result is remembered at that stat. The stat is from before
// the read, so a write between the two is a different stat next time.
// A nil memo reads every time.
func Identify[T any](memo Memo, ref Ref, read func() (T, error)) (string, T, error) {
	var ident T
	if memo != nil {
		if s, ok := memo.Recall(ref.AbsPath, ref.Stat()); ok && json.Unmarshal(s.Ident, &ident) == nil {
			return s.SHA256, ident, nil
		}
	}
	ident, err := read()
	if err != nil {
		return "", ident, err
	}
	sum, err := HashFile(ref.AbsPath)
	if err != nil {
		return "", ident, err
	}
	if memo != nil {
		if raw, err := json.Marshal(ident); err == nil {
			memo.Remember(ref.AbsPath, ref.Stat(), Seen{SHA256: sum, Ident: raw})
		}
	}
	return sum, ident, nil
}
