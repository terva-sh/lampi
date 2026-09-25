package watermark

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// A store an earlier release wrote gains the ruleset and hits columns.
// Its rows read as a prefix that was not scanned, and a commit then
// records both.
func TestOpenAddsScanColumnsToAnOldStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watermarks.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("a", 64)
	if _, err := old.Exec(`CREATE TABLE watermarks (
		machine_id TEXT NOT NULL, harness TEXT NOT NULL, root_path TEXT NOT NULL,
		relative_path TEXT NOT NULL, size INTEGER NOT NULL, mtime_unix_nano INTEGER NOT NULL,
		sha256 TEXT NOT NULL, byte_offset INTEGER NOT NULL, updated_at TEXT NOT NULL,
		PRIMARY KEY (machine_id, harness, root_path, relative_path))`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO watermarks VALUES ('m', 'terva', '/r', 's.jsonl', 5, 0, ?, 5, '2026-09-25T00:00:00Z')`, sum); err != nil {
		t.Fatal(err)
	}
	old.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	key := Mark{MachineID: "m", Harness: "terva", Root: "/r", RelPath: "s.jsonl"}
	got, ok, err := s.Get(ctx, key)
	if err != nil || !ok || got.SHA256 != sum || got.Ruleset != "" || got.Hits != 0 {
		t.Fatalf("old row %+v ok=%v err=%v", got, ok, err)
	}
	got.Ruleset, got.Hits = "v2", 2
	if err := s.Commit(ctx, got, protocol.ManifestAck{SessionUID: "u"}); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Get(ctx, key)
	if err != nil || got.Ruleset != "v2" || got.Hits != 2 {
		t.Fatalf("committed %+v err=%v", got, err)
	}
	s.Close()
	// A second open finds the columns and adds nothing.
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}
