package cursorcli

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/adapter/sqlitesnap"
	"terva.sh/lampi/internal/protocol"
)

type statMemo map[string]struct {
	st   adapter.FileStat
	seen adapter.Seen
}

func (m statMemo) Recall(path string, st adapter.FileStat) (adapter.Seen, bool) {
	e, ok := m[path]
	if !ok || e.st.Size != st.Size || e.st.Inode != st.Inode || !e.st.ModTime.Equal(st.ModTime) {
		return adapter.Seen{}, false
	}
	return e.seen, true
}

func (m statMemo) Remember(path string, st adapter.FileStat, s adapter.Seen) {
	m[path] = struct {
		st   adapter.FileStat
		seen adapter.Seen
	}{st, s}
}

// A store.db and WAL with the stat the memo saw are not snapshotted.
// The manifest keeps the digest, size, and hidden scan of the export;
// Load builds that export when the upload asks for the bytes. A write
// to the WAL is a new stat and a new export.
func TestManifestsMemoSkipsAnUnchangedStore(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "chats", "ab12", "sid-1", "store.db")
	if err := seedClosed(dbPath); err != nil {
		t.Fatal(err)
	}
	pat := "ghp_" + strings.Repeat("Q", 36)
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA wal_autocheckpoint=0`} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES ('hidden', ?)`, []byte("\xff\xfe"+pat+"\x00")); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(filepath.Dir(dbPath), "meta.json"), `{"cwd":"/work/app"}`)

	copies := 0
	takeSnapshot = func(ctx context.Context, src, prefix string) (*sqlitesnap.Snapshot, error) {
		copies++
		return sqlitesnap.Take(ctx, src, prefix)
	}
	t.Cleanup(func() { takeSnapshot = sqlitesnap.Take })
	memo := statMemo{}
	permit := func(protocol.Manifest) bool { return true }

	first, err := ManifestsMemo(root, "machine-1", memo, permit, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup()
	a := first.Manifests[0].Artifacts[0]
	if copies != 1 || first.Paths[a.RelPath] == "" || first.Load != nil {
		t.Fatalf("first pass: copies %d paths %v load %v", copies, first.Paths, first.Load != nil)
	}
	if first.Hidden[a.SHA256].Hits != 1 {
		t.Fatalf("first pass hidden %+v", first.Hidden)
	}

	second, err := ManifestsMemo(root, "machine-1", memo, permit, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Cleanup()
	b := second.Manifests[0].Artifacts[0]
	if copies != 1 {
		t.Fatalf("unchanged store was snapshotted again: %d", copies)
	}
	if b.SHA256 != a.SHA256 || b.Size != a.Size || b.TailSHA256 != a.TailSHA256 || second.Paths[b.RelPath] != "" {
		t.Fatalf("recalled %+v, want %+v without a path", b, a)
	}
	if second.Hidden[b.SHA256].Hits != 1 {
		t.Fatalf("recalled hidden %+v", second.Hidden)
	}
	if second.Load == nil {
		t.Fatal("no Load for a recalled session")
	}
	p, err := second.Load(b.RelPath)
	if err != nil {
		t.Fatal(err)
	}
	if sum, err := adapter.HashFile(p); err != nil || sum != a.SHA256 || copies != 2 {
		t.Fatalf("load: sum %s err %v copies %d", sum, err, copies)
	}
	if _, err := second.Load("chats/ab12/other/store.json"); err == nil {
		t.Fatal("loaded a session that is not in the bundle")
	}

	// A WAL write keeps store.db's stat. The WAL stat is what changes.
	time.Sleep(10 * time.Millisecond)
	if _, err := db.Exec(`INSERT INTO blobs (id, data) VALUES ('later', '{"text":"later"}')`); err != nil {
		t.Fatal(err)
	}
	third, err := ManifestsMemo(root, "machine-1", memo, permit, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Cleanup()
	c := third.Manifests[0].Artifacts[0]
	if copies != 3 || c.SHA256 == a.SHA256 || third.Paths[c.RelPath] == "" {
		t.Fatalf("WAL write was not exported: copies %d %+v", copies, c)
	}
	if _, err := os.Stat(dbPath + "-wal"); err != nil {
		t.Fatalf("test wrote no WAL: %v", err)
	}
}
