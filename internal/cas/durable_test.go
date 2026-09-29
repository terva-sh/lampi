package cas

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// recordSyncs replaces syncDir for the test. The returned func lists the
// directories flushed so far, each with whether object existed when that
// directory was flushed.
func recordSyncs(t *testing.T, object string) func() map[string]bool {
	t.Helper()
	var mu sync.Mutex
	seen := map[string]bool{}
	prev := syncDir
	syncDir = func(dir string) error {
		_, err := os.Stat(object)
		mu.Lock()
		seen[dir] = seen[dir] || err == nil
		mu.Unlock()
		return prev(dir)
	}
	t.Cleanup(func() { syncDir = prev })
	return func() map[string]bool {
		mu.Lock()
		defer mu.Unlock()
		out := map[string]bool{}
		for k, v := range seen {
			out[k] = v
		}
		return out
	}
}

func putAll(t *testing.T, s *Store, bodies ...[]byte) {
	t.Helper()
	for _, b := range bodies {
		if _, err := s.Put(digestOf(b), bytes.NewReader(b), 0); err != nil {
			t.Fatal(err)
		}
	}
}

// objectForms are the two names an object can have: the compressed one
// every install writes, and the raw one installed before compression.
var objectForms = map[string]func(s *Store, d string) (string, error){
	"compressed": (*Store).zstPath,
	"raw":        (*Store).Path,
}

// installedAs checks that digest reads as want and is stored in the
// compressed form only.
func installedAs(t *testing.T, s *Store, d string, want []byte) {
	t.Helper()
	got, err := s.Read(d)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("object reads %q, want %q", got, want)
	}
	raw, _ := s.Path(d)
	if _, err := os.Lstat(raw); !os.IsNotExist(err) {
		t.Fatalf("raw object left beside the compressed one: %v", err)
	}
	if _, err := os.Lstat(raw + zstSuffix); err != nil {
		t.Fatalf("no compressed object: %v", err)
	}
}

// storeFrame replaces digest's object with a well-formed frame of
// content: an object whose bytes are wrong, or shorter than they were.
func storeFrame(t *testing.T, s *Store, d string, content []byte) string {
	t.Helper()
	p, _ := s.zstPath(d)
	var buf bytes.Buffer
	if err := encode(&buf, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatal(err)
	}
	damage(t, p, buf.Bytes())
	return p
}

// objectBytes sums what every object holds once decompressed.
func objectBytes(t *testing.T, s *Store) int64 {
	t.Helper()
	var held int64
	err := s.Entries(func(e Entry) error {
		if e.Logical {
			return nil
		}
		n, _, err := s.ObjectSize(e.Digest)
		held += n
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return held
}

func damage(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPutSyncsShardAndDirectoryAfterRename(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("durable\n")
	d := digestOf(body)
	final, _ := s.zstPath(d)
	syncs := recordSyncs(t, final)

	if _, err := s.Put(d, bytes.NewReader(body), 1024); err != nil {
		t.Fatal(err)
	}
	got := syncs()
	if _, ok := got[filepath.Join(root, "sha256")]; !ok {
		t.Fatalf("new shard directory not flushed to its parent: %v", got)
	}
	if !got[filepath.Dir(final)] {
		t.Fatalf("shard directory not flushed after the rename: %v", got)
	}
}

func TestConcatRangeAndLogicalSyncDirectory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	left, right := []byte("hello "), []byte("world")
	whole := append(append([]byte{}, left...), right...)
	full := digestOf(whole)
	putAll(t, s, left, right)

	final, _ := s.zstPath(full)
	syncs := recordSyncs(t, final)
	if _, err := s.Concat(full, []string{digestOf(left), digestOf(right)}, 0); err != nil {
		t.Fatal(err)
	}
	if !syncs()[filepath.Dir(final)] {
		t.Fatalf("concat did not flush the directory after the rename: %v", syncs())
	}

	ranged := []byte("ranged body")
	rd := digestOf(ranged)
	rfinal, _ := s.zstPath(rd)
	syncs = recordSyncs(t, rfinal)
	n := int64(len(ranged))
	if _, complete, err := s.PutRange(rd, 0, n-1, n, 0, bytes.NewReader(ranged)); err != nil || !complete {
		t.Fatalf("range complete %v err %v", complete, err)
	}
	if !syncs()[filepath.Dir(rfinal)] {
		t.Fatalf("range install did not flush the directory after the rename: %v", syncs())
	}

	a, b := []byte("lo"), []byte("gical")
	putAll(t, s, a, b)
	ld := digestOf(append(append([]byte{}, a...), b...))
	lp, _ := s.logicalPath(ld)
	syncs = recordSyncs(t, lp)
	if _, err := s.BindLogical(ld, []string{digestOf(a), digestOf(b)}, []int64{2, 5}); err != nil {
		t.Fatal(err)
	}
	if !syncs()[filepath.Dir(lp)] {
		t.Fatalf("logical index not flushed after the rename: %v", syncs())
	}
}

func TestPartialMetaSyncsDirectoryAfterRename(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("two ranges")
	d := digestOf(body)
	meta := filepath.Join(s.partialDir(d), "meta.json")
	syncs := recordSyncs(t, meta)
	n := int64(len(body))
	if _, complete, err := s.PutRange(d, 0, 3, n, 0, bytes.NewReader(body[:4])); err != nil || complete {
		t.Fatalf("first range complete %v err %v", complete, err)
	}
	if !syncs()[s.partialDir(d)] {
		t.Fatalf("partial directory not flushed after meta.json was renamed: %v", syncs())
	}
}

func TestPutRepairsDamagedObject(t *testing.T) {
	body := []byte("the right bytes\n")
	d := digestOf(body)
	for name, bad := range map[string][]byte{
		"truncated":   body[:4],
		"empty":       {},
		"zero-filled": make([]byte, len(body)),
	} {
		for form, path := range objectForms {
			t.Run(form+"/"+name, func(t *testing.T) {
				s, err := Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				final, _ := path(s, d)
				damage(t, final, bad)

				exists, err := s.Put(d, bytes.NewReader(body), 1024)
				if err != nil {
					t.Fatal(err)
				}
				if exists {
					t.Fatal("put over a damaged object reported exists")
				}
				installedAs(t, s, d, body)
				if exists, err := s.Put(d, bytes.NewReader(body), 1024); err != nil || !exists {
					t.Fatalf("put over the repaired object: exists %v err %v", exists, err)
				}
				leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(final), ".put-*"))
				if len(leftovers) != 0 {
					t.Fatalf("temp files left: %v", leftovers)
				}
			})
		}
	}
}

func TestConcatAndRangeRepairDamagedObject(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	left, right := []byte("hello "), []byte("world")
	whole := append(append([]byte{}, left...), right...)
	full := digestOf(whole)
	putAll(t, s, left, right)
	for form, path := range objectForms {
		if err := s.RemoveEntry(Entry{Digest: full}); err != nil {
			t.Fatal(err)
		}
		final, _ := path(s, full)
		damage(t, final, make([]byte, len(whole)))
		exists, err := s.Concat(full, []string{digestOf(left), digestOf(right)}, 0)
		if err != nil || exists {
			t.Fatalf("%s: concat over damaged object: exists %v err %v", form, exists, err)
		}
		installedAs(t, s, full, whole)

		if err := s.RemoveEntry(Entry{Digest: full}); err != nil {
			t.Fatal(err)
		}
		damage(t, final, whole[:3])
		total := int64(len(whole))
		exists, complete, err := s.PutRange(full, 0, total-1, total, 0, bytes.NewReader(whole))
		if err != nil || exists || !complete {
			t.Fatalf("%s: range over damaged object: exists %v complete %v err %v", form, exists, complete, err)
		}
		installedAs(t, s, full, whole)
	}
}

func TestBindLogicalDropsDamagedObject(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	left, right := []byte("hello "), []byte("world")
	whole := append(append([]byte{}, left...), right...)
	full := digestOf(whole)
	putAll(t, s, left, right)
	final, _ := s.Path(full)
	damage(t, final, whole[:2])

	exists, err := s.BindLogical(full, []string{digestOf(left), digestOf(right)}, []int64{int64(len(left)), int64(len(right))})
	if err != nil || exists {
		t.Fatalf("bind over damaged object: exists %v err %v", exists, err)
	}
	got, err := s.Read(full)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, whole) {
		t.Fatalf("read %q", got)
	}
}

// blockingReader returns body only after release is closed.
type blockingReader struct {
	started chan struct{}
	release chan struct{}
	body    []byte
	once    sync.Once
	done    bool
}

func (r *blockingReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	return copy(p, r.body), nil
}

func TestSlowBodyDoesNotBlockOtherPut(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	slowBody := []byte("slow client body")
	slow := &blockingReader{started: make(chan struct{}), release: make(chan struct{}), body: slowBody}
	slowDone := make(chan error, 1)
	go func() {
		_, err := s.Put(digestOf(slowBody), slow, 1024)
		slowDone <- err
	}()
	<-slow.started

	fast := []byte("9 bytes!\n")
	fastDone := make(chan error, 1)
	go func() {
		_, err := s.Put(digestOf(fast), bytes.NewReader(fast), 1024)
		fastDone <- err
	}()
	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		close(slow.release)
		<-slowDone
		t.Fatal("a put waited behind another client's slow body")
	}

	close(slow.release)
	if err := <-slowDone; err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Has(digestOf(slowBody)); err != nil || !ok {
		t.Fatalf("slow put has %v err %v", ok, err)
	}
}

// A crash can leave an installed object with no bytes. Has reports it
// missing so blobs/check asks the client for it again, and the put
// repairs it. An empty object under the empty digest is still present.
func TestHasReportsEmptyDamagedObjectMissing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("hello transcript\n")
	digest, _, err := Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putAll(t, s, body, nil)
	p, err := s.zstPath(digest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(p, 0); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Has(digest); err != nil || ok {
		t.Fatalf("Has(truncated) = %v, %v; want false", ok, err)
	}
	if ok, err := s.Has(emptyDigest); err != nil || !ok {
		t.Fatalf("Has(empty digest) = %v, %v; want true", ok, err)
	}
	exists, err := s.Put(digest, bytes.NewReader(body), 0)
	if err != nil || exists {
		t.Fatalf("Put = %v, %v; want stored", exists, err)
	}
	if ok, err := s.Has(digest); err != nil || !ok {
		t.Fatalf("Has(repaired) = %v, %v; want true", ok, err)
	}
}

// Replacing a raw object with a frame makes the frame's directory entry
// durable before the raw file is removed, so a crash between the two
// leaves at least one copy (review 1295).
func TestFrameIsDurableBeforeTheRawCopyGoes(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("raw before, framed after\n")
	d := digestOf(body)
	raw, _ := s.Path(d)
	damage(t, raw, []byte("damaged raw bytes, same len"))
	var rawAtSync []bool
	prev := syncDir
	syncDir = func(dir string) error {
		_, err := os.Lstat(raw)
		rawAtSync = append(rawAtSync, err == nil)
		return prev(dir)
	}
	t.Cleanup(func() { syncDir = prev })
	if _, err := s.Put(d, bytes.NewReader(body), 0); err != nil {
		t.Fatal(err)
	}
	if len(rawAtSync) < 2 || !rawAtSync[len(rawAtSync)-2] || rawAtSync[len(rawAtSync)-1] {
		t.Fatalf("raw file present at each directory sync: %v; want a sync with it still there, then one after it goes", rawAtSync)
	}
	installedAs(t, s, d, body)
}
