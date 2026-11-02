package web

import (
	"html"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M6F7JMQYGY3WGGV55H0M3FR5: owners with two or more repositories
// and folders where two or more project folders branch come first, then
// each repository and folder, most sessions first.
func TestProjectOptions(t *testing.T) {
	got := projectOptions([]catalog.ProjectChoice{
		{Remote: "github.com/acme/app", Sessions: 5},
		{Remote: "github.com/acme/lib", Sessions: 2},
		{Remote: "github.com/solo/one", Sessions: 9},
		{Folder: "/home/me/.t3/worktrees/lampi/t3-1", Sessions: 1},
		{Folder: "/home/me/.t3/worktrees/lampi/t3-2", Sessions: 1},
		{Folder: "/home/me/.t3/worktrees/ledger/t3-9", Sessions: 3},
	})
	var values []string
	for _, o := range got {
		values = append(values, o.Value+" = "+o.Label)
	}
	want := []string{
		"github.com/acme = owner · 2 repositories · 7 sessions",
		"/home/me/.t3/worktrees = folder · 3 project folders · 5 sessions",
		"/home/me/.t3/worktrees/lampi = folder · 2 project folders · 2 sessions",
		"github.com/solo/one = repository · 9 sessions",
		"github.com/acme/app = repository · 5 sessions",
		"/home/me/.t3/worktrees/ledger/t3-9 = folder · 3 sessions",
		"github.com/acme/lib = repository · 2 sessions",
		"/home/me/.t3/worktrees/lampi/t3-1 = folder · 1 session",
		"/home/me/.t3/worktrees/lampi/t3-2 = folder · 1 session",
	}
	if !slices.Equal(values, want) {
		t.Errorf("options\n%s\nwant\n%s", strings.Join(values, "\n"), strings.Join(want, "\n"))
	}
}

// The sessions page filters by the Project box, as a remote in any form
// or a folder, and by machine, and lists the choices.
func TestSessionsPickers(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	now := time.Now()
	add := func(machine, native string, p protocol.Project) {
		t.Helper()
		m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: machine, Harness: "claude", NativeSessionID: native, Project: p,
			Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/" + native + ".jsonl", SHA256: putBlob(t, lake, []byte(native+"\n")), Size: int64(len(native) + 1)}}}
		if _, err := lake.Catalog.Ingest(ctx, m, now, []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	add("machine-a", "s-app", protocol.Project{GitRemote: "git@github.com:acme/app.git", CWD: "/w/app"})
	add("machine-b", "s-lib", protocol.Project{GitRemote: "https://github.com/acme/lib", CWD: "/w/lib"})
	add("machine-a", "s-wt1", protocol.Project{CWD: "/home/me/.t3/worktrees/lampi/t3-1"})
	add("machine-a", "s-wt2", protocol.Project{CWD: "/home/me/.t3/worktrees/lampi/t3-2"})
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("b", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.BindMachine(ctx, ds[0].ID, "machine-a", now); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	page := html.UnescapeString(get(h, "/sessions", admin).Body.String())
	for _, want := range []string{
		`<option value="github.com/acme" label="owner · 2 repositories · 2 sessions">`,
		`<option value="/home/me/.t3/worktrees/lampi" label="folder · 2 project folders · 2 sessions">`,
		`<option value="machine-a" >laptop</option>`,
		`<option value="machine-b" >machine-b</option>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("sessions page lacks %q", want)
		}
	}
	shows := func(q string) []string {
		t.Helper()
		w := get(h, "/sessions?"+q, admin)
		if w.Code != 200 {
			t.Fatalf("%s: %d", q, w.Code)
		}
		var got []string
		for _, n := range []string{"s-app", "s-lib", "s-wt1", "s-wt2"} {
			if strings.Contains(w.Body.String(), ">"+n+"</a>") {
				got = append(got, n)
			}
		}
		return got
	}
	for q, want := range map[string][]string{
		"match=" + url.QueryEscape("git@github.com:acme"):     {"s-app", "s-lib"},
		"match=" + url.QueryEscape("github.com/acme/app"):     {"s-app"},
		"match=" + url.QueryEscape("/home/me/.t3/worktrees"):  {"s-wt1", "s-wt2"},
		"folder=" + url.QueryEscape("/home/me/.t3/worktrees"): {"s-wt1", "s-wt2"},
		"remote=github.com/acme&machine=machine-b":            {"s-lib"},
		"machine=machine-a": {"s-app", "s-wt1", "s-wt2"},
	} {
		if got := shows(q); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", q, got, want)
		}
	}
	if w := get(h, "/sessions?match="+url.QueryEscape("/home/me/.t3/worktrees"), admin); !strings.Contains(w.Body.String(), `name="match" list="project-choices" value="/home/me/.t3/worktrees"`) {
		t.Error("the Project box does not keep the folder")
	}
	for _, q := range []string{"match=/", "match=x&remote=y", "folder=relative/dir"} {
		if w := get(h, "/sessions?"+q, admin); w.Code != 400 {
			t.Errorf("%s: %d, want 400", q, w.Code)
		}
	}
}
