package discover

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// A file removed between the directory read and its stat is one skip.
// The rest of the tree is still listed.
func TestWalkFilesSkipsAFileRemovedMidWalk(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	skipped, err := WalkFiles(base, func(p string, d fs.DirEntry) error {
		if d.Name() == "a" {
			if err := os.Remove(filepath.Join(base, "b")); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := d.Info(); err != nil {
			return err
		}
		got = append(got, d.Name())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || !errors.Is(skipped[0], fs.ErrNotExist) {
		t.Fatalf("skipped %v", skipped)
	}
	var pe *fs.PathError
	if !errors.As(skipped[0], &pe) || pe.Path != filepath.Join(base, "b") {
		t.Fatalf("skip does not name the file: %v", skipped[0])
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("walked %v", got)
	}
}

// An unreadable directory is skipped and named. Root reads through the
// mode bits, so this needs another user.
func TestWalkFilesSkipsAnUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 000 directory")
	}
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "x.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "ok.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	var got []string
	skipped, err := WalkFiles(base, func(p string, d fs.DirEntry) error {
		got = append(got, d.Name())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "ok.jsonl" {
		t.Fatalf("walked %v", got)
	}
	if len(skipped) != 1 || !errors.Is(skipped[0], fs.ErrPermission) {
		t.Fatalf("skipped %v", skipped)
	}
}

// fn saying permission denied for one entry skips that entry only. This
// is the same path as an unreadable file, without needing a non-root user.
func TestWalkFilesSkipsWhatFnCannotRead(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(base, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	skipped, err := WalkFiles(base, func(p string, d fs.DirEntry) error {
		if d.Name() == "a" {
			return &fs.PathError{Op: "open", Path: p, Err: fs.ErrPermission}
		}
		n++
		return nil
	})
	if err != nil || n != 1 || len(skipped) != 1 {
		t.Fatalf("n %d skipped %v err %v", n, skipped, err)
	}
}

// A symlinked base is followed, and paths stay under the link.
func TestWalkFilesFollowsASymlinkedBase(t *testing.T) {
	target := t.TempDir()
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sub", "s.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	var got []string
	var dirs []string
	if _, err := WalkFiles(link, func(p string, _ fs.DirEntry) error {
		got = append(got, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WalkDirs(link, func(p string) error {
		dirs = append(dirs, p)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != filepath.Join(link, "sub", "s.jsonl") {
		t.Fatalf("files %v", got)
	}
	if len(dirs) != 1 || dirs[0] != filepath.Join(link, "sub") {
		t.Fatalf("dirs %v", dirs)
	}
}

// A session file that is a symlink is described by its target, so a
// target that grows is a new size and mtime, not the link's.
func TestSessionsStatASymlinkTarget(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(t.TempDir(), "real.jsonl")
	if err := os.WriteFile(target, []byte("{\"type\":\"meta\"}\n{\"type\":\"message\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "s.jsonl")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	files, err := Sessions(home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Size != st.Size() || !files[0].ModTime.Equal(st.ModTime().UTC()) {
		t.Fatalf("files %+v, target size %d", files, st.Size())
	}
}
