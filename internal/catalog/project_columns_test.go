package catalog

import (
	"context"
	"slices"
	"strconv"
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
	return projectSessionOn(t, c, "machine-a", native, p)
}

func projectSessionOn(t *testing.T, c *Catalog, machine, native string, p protocol.Project) string {
	t.Helper()
	body := []byte(`{"session":"` + native + `"}` + "\n")
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: machine, Harness: protocol.HarnessTerva, NativeSessionID: native, Project: p,
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

// TKT-01M6F7JMQYGY3WGGV55H0M3FR5: the sessions list narrows by remote
// prefix, folder prefix and machine, a prefix matching whole segments
// only, and the choices list each remote and each folder without one.
func TestSessionPrefixFilters(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	app := projectSession(t, c, "s-app", protocol.Project{GitRemote: "git@github.com:acme/app.git", CWD: "/work/app"})
	lib := projectSessionOn(t, c, "machine-b", "s-lib", protocol.Project{GitRemote: "https://github.com/acme/lib", CWD: "/work/lib"})
	projectSession(t, c, "s-x", protocol.Project{GitRemote: "github.com/acmex/z", CWD: "/work/x"})
	wa := projectSession(t, c, "s-wa", protocol.Project{CWD: "/scratch/a"})
	wa2 := projectSession(t, c, "s-wa2", protocol.Project{CWD: "/scratch/a/deep"})
	projectSession(t, c, "s-wab", protocol.Project{CWD: "/scratch/ab"})
	uids := func(r PageRequest) []string {
		t.Helper()
		p, err := c.DashboardSessions(ctx, AllBays(), r)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, s := range p.Items {
			out = append(out, s.UID)
		}
		slices.Sort(out)
		return out
	}
	sorted := func(s ...string) []string { slices.Sort(s); return s }
	for name, c := range map[string]struct {
		r    PageRequest
		want []string
	}{
		"owner":        {PageRequest{Remote: "github.com/acme"}, sorted(app, lib)},
		"repository":   {PageRequest{Remote: "github.com/acme/app"}, sorted(app)},
		"folder":       {PageRequest{Folder: "/scratch/a"}, sorted(wa, wa2)},
		"folder slash": {PageRequest{Folder: "/scratch/a/"}, sorted(wa, wa2)},
		"machine":      {PageRequest{Machine: "machine-b"}, sorted(lib)},
		"both":         {PageRequest{Remote: "github.com/acme", Machine: "machine-a"}, sorted(app)},
	} {
		if got := uids(c.r); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	for _, r := range []PageRequest{{Folder: "/"}, {Folder: "scratch"}, {Remote: "github.com/"}} {
		if _, err := c.DashboardSessions(ctx, AllBays(), r); err != ErrPage {
			t.Errorf("%+v: %v, want ErrPage", r, err)
		}
	}
	projects, machines, err := c.ProjectChoices(ctx, AllBays())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(machines, []string{"machine-a", "machine-b"}) {
		t.Errorf("machines %v", machines)
	}
	var got []string
	for _, p := range projects {
		got = append(got, p.Remote+"|"+p.Folder+"|"+strconv.Itoa(p.Sessions))
	}
	slices.Sort(got)
	if want := sorted("|/scratch/a/deep|1", "|/scratch/a|1", "|/scratch/ab|1", "github.com/acme/app||1", "github.com/acme/lib||1", "github.com/acmex/z||1"); !slices.Equal(got, want) {
		t.Errorf("choices %v, want %v", got, want)
	}
}
