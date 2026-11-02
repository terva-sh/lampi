package catalog

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// dropProjectColumns takes a current catalog back to before migration
// 22, for a test that rolls the schema back to an older version.
const dropProjectColumns = `DROP INDEX sessions_by_remote; DROP INDEX sessions_by_cwd; DROP INDEX provenance_by_machine; ALTER TABLE sessions DROP COLUMN project_remote; ALTER TABLE sessions DROP COLUMN project_cwd; `

func projectSession(t *testing.T, c *Catalog, native string, p protocol.Project) string {
	t.Helper()
	body := []byte(`{"session":"` + native + `"}` + "\n")
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessTerva, NativeSessionID: native, Project: p,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/" + native + ".jsonl", Size: int64(len(body)), SHA256: digestHex(body)}}}
	ack, err := c.Ingest(context.Background(), m, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, memBlobs{digestHex(body): body})
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

func columnsOf(t *testing.T, c *Catalog, uid string) (remote, cwd string) {
	t.Helper()
	if err := c.db.QueryRow(`SELECT project_remote, project_cwd FROM sessions WHERE session_uid=?`, uid).Scan(&remote, &cwd); err != nil {
		t.Fatal(err)
	}
	return remote, cwd
}

// TKT-01M6F7JMM85DPVQ88YSC24TENC: ingest writes each session's
// normalized remote and folder; migration 22 fills them for the
// sessions a lake already holds, and the filters' queries use the new
// indexes.
func TestProjectColumns(t *testing.T) {
	c, path := openTemp(t)
	remote := "git@github.com:acme/app.git"
	want := protocol.NormalizeRemote(remote)
	a := projectSession(t, c, "sess-a", protocol.Project{GitRemote: remote, CWD: "/work/app/"})
	b := projectSession(t, c, "sess-b", protocol.Project{CWD: "/tmp/x"})
	if r, d := columnsOf(t, c, a); r != want || d != "/work/app" {
		t.Fatalf("ingest wrote %q %q, want %q /work/app", r, d, want)
	}
	if r, d := columnsOf(t, c, b); r != "" || d != "/tmp/x" {
		t.Fatalf("ingest wrote %q %q for a folder project", r, d)
	}

	// A later head with a new project, and a post that only learns the
	// project id, rewrite the columns with the manifest.
	moved := protocol.Project{GitRemote: "https://git.example/team/lib", CWD: "/work/lib"}
	old := []byte(`{"session":"sess-b"}` + "\n")
	body := append(append([]byte{}, old...), `{"more":1}`+"\n"...)
	blobs := memBlobs{digestHex(body): body, digestHex(old): old}
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessTerva, NativeSessionID: "sess-b", Project: moved,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/sess-b.jsonl", Size: int64(len(body)), SHA256: digestHex(body)}}}
	if _, err := c.Ingest(context.Background(), m, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, blobs); err != nil {
		t.Fatal(err)
	}
	if r, d := columnsOf(t, c, b); r != protocol.NormalizeRemote(moved.GitRemote) || d != "/work/lib" {
		t.Fatalf("a moved head wrote %q %q", r, d)
	}
	m.Project.CWD, m.Project.GitRoot = "/work/lib2", strings.Repeat("a", 40)
	if _, err := c.Ingest(context.Background(), m, time.Now(), []Decision{{Relation: protocol.RelationHead}}, blobs); err != nil {
		t.Fatal(err)
	}
	if _, d := columnsOf(t, c, b); d != "/work/lib2" {
		t.Fatalf("a learned project id left the folder at %q", d)
	}

	// Back to schema 21, with a session whose manifest cannot be read.
	if _, err := c.db.Exec(dropProjectColumns + `PRAGMA user_version = 21`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`INSERT INTO sessions(session_uid,harness,native_session_id,head_sha256,manifest_json,ingested_at) VALUES('bad','terva','bad','head','not json','2026-11-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if m := c.Migrated(); m.From != 21 || !slices.Equal(m.Steps, []string{"migrateProjectColumns"}) {
		t.Fatalf("migration %+v", m)
	}
	if r, d := columnsOf(t, c, a); r != want || d != "/work/app" {
		t.Errorf("migration filled %q %q", r, d)
	}
	if r, d := columnsOf(t, c, "bad"); r != "" || d != "" {
		t.Errorf("an unreadable manifest filled %q %q", r, d)
	}
	for q, index := range map[string]string{
		`SELECT session_uid FROM sessions WHERE project_remote >= 'github.com/' AND project_remote < 'github.com0' ORDER BY web_updated_ns DESC`: "sessions_by_remote",
		`SELECT session_uid FROM sessions WHERE project_cwd = '/work/app' ORDER BY web_updated_ns DESC`:                                          "sessions_by_cwd",
		`SELECT session_uid FROM provenance WHERE machine_id = 'machine-a'`:                                                                      "provenance_by_machine",
	} {
		var plan strings.Builder
		rows, err := c.db.Query(`EXPLAIN QUERY PLAN ` + q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(detail + "; ")
		}
		rows.Close()
		if !strings.Contains(plan.String(), index) {
			t.Errorf("%s plans %s, not using %s", q, plan.String(), index)
		}
	}
}
