package upload

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// checkout makes a directory that reads as a git checkout whose origin
// is remote.
func checkout(t *testing.T, dir, remote string) {
	t.Helper()
	mustFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustFile(t, filepath.Join(dir, ".git", "config"), "[remote \"origin\"]\n\turl = "+remote+"\n")
}

func tervaSession(t *testing.T, home, bucket, id, cwd string) {
	t.Helper()
	writeSession(t, home, bucket, id+".jsonl", []byte(`{"type":"meta","meta":{"id":"`+id+`","cwd":"`+cwd+`"}}`+"\n"))
}

// TKT-01M3HKYFF: one line per refused project. Checkouts of one
// repository are one project; an allowed or denied session is reported
// with its reason, and nothing is written.
func TestRefusalsGroupsByRepository(t *testing.T) {
	work := t.TempDir()
	a, b := filepath.Join(work, "a"), filepath.Join(work, "b")
	checkout(t, a, "git@git.example:team/app.git")
	checkout(t, b, "https://git.example/team/app")
	secret := filepath.Join(work, "secret")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(work, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", a)
	tervaSession(t, home, "bbbb", "s2", b)
	tervaSession(t, home, "cccc", "s3", b)
	tervaSession(t, home, "dddd", "s4", secret)
	tervaSession(t, home, "eeee", "s5", allowed)
	state := t.TempDir()

	got, skipped := Refusals(Options{
		TervaHome: home,
		MachineID: "m",
		StateDir:  state,
		Projects: config.Projects{
			Allow: []config.ProjectMatch{{CWDPrefix: allowed}, {CWDPrefix: secret}},
			Deny:  []config.ProjectMatch{{CWDPrefix: secret}},
		},
	})
	if len(skipped) != 0 {
		t.Fatalf("skipped %v", skipped)
	}
	if len(got) != 2 {
		t.Fatalf("got %d projects, want the repository and the denied cwd: %+v", len(got), got)
	}
	repo, denied := got[0], got[1]
	if repo.Sessions != 3 || repo.CWDs != 2 || repo.CWD != a || repo.Reason != config.RefusedNoMatch ||
		len(repo.Harnesses) != 1 || repo.Harnesses[0] != "terva" {
		t.Errorf("repository line %+v", repo)
	}
	if denied.Sessions != 1 || denied.CWD != secret || denied.GitRemote != "" || denied.Reason != config.RefusedByDeny {
		t.Errorf("denied line %+v", denied)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Errorf("the report wrote to the state directory: %v %v", entries, err)
	}
}

func TestRefusalReasons(t *testing.T) {
	cases := []struct {
		name string
		p    config.Projects
		id   config.ProjectID
		want string
	}{
		{"allowed", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{CWD: "/w/a"}, ""},
		{"empty allow", config.Projects{}, config.ProjectID{CWD: "/w/a"}, config.RefusedEmptyAllow},
		{"no cwd", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{}, config.RefusedNoCWD},
		{"no match", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}}, config.ProjectID{CWD: "/x"}, config.RefusedNoMatch},
		{"deny wins", config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/w"}}, Deny: []config.ProjectMatch{{CWDPrefix: "/w/a"}}}, config.ProjectID{CWD: "/w/a"}, config.RefusedByDeny},
	}
	for _, c := range cases {
		if got := c.p.Refusal(c.id); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if permitted := c.p.Permitted(c.id); permitted != (c.want == "") {
			t.Errorf("%s: Permitted %v disagrees with Refusal %q", c.name, permitted, c.want)
		}
	}
}

// Checkouts of one repository refused for different reasons are
// separate lines, each with its own true reason.
func TestRefusalsSplitsARepositoryByReason(t *testing.T) {
	work := t.TempDir()
	open, closed := filepath.Join(work, "open"), filepath.Join(work, "closed")
	checkout(t, open, "git@git.example:team/app.git")
	checkout(t, closed, "git@git.example:team/app.git")
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", open)
	tervaSession(t, home, "bbbb", "s2", closed)
	got, _ := Refusals(Options{
		TervaHome: home,
		MachineID: "m",
		Projects: config.Projects{
			Allow: []config.ProjectMatch{{CWDPrefix: closed}},
			Deny:  []config.ProjectMatch{{CWDPrefix: closed}},
		},
	})
	if len(got) != 2 {
		t.Fatalf("got %+v, want one line per reason", got)
	}
	reasons := map[string]string{got[0].CWD: got[0].Reason, got[1].CWD: got[1].Reason}
	if reasons[open] != config.RefusedNoMatch || reasons[closed] != config.RefusedByDeny {
		t.Fatalf("reasons %v", reasons)
	}
}

// TKT-01M3M7M0TH: the inventory groups allowed projects as Refusals
// groups refused ones, and its refused rows are Refusals' lines, so
// the dashboard and agent refused cannot disagree.
func TestInventoryRowsAgreeWithRefusals(t *testing.T) {
	work := t.TempDir()
	a, b := filepath.Join(work, "a"), filepath.Join(work, "b")
	checkout(t, a, "git@git.example:team/app.git")
	checkout(t, b, "https://git.example/team/app")
	allowed := filepath.Join(work, "allowed")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", a)
	tervaSession(t, home, "bbbb", "s2", b)
	tervaSession(t, home, "cccc", "s3", allowed)
	tervaSession(t, home, "dddd", "s4", allowed)
	opt := Options{TervaHome: home, MachineID: "m", Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: allowed}}}}

	refused, _ := Refusals(opt)
	bundles, _ := bundlesFor(opt)
	defer cleanupBundles(bundles)
	rows := inventoryRows(opt, bundles)
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	var fromRows []RefusedProject
	for _, r := range rows {
		if r.Reason != "" {
			fromRows = append(fromRows, RefusedProject{CWD: r.CWD, CWDs: r.CWDs, GitRemote: r.GitRemote, Harnesses: r.Harnesses, Sessions: r.Sessions, Reason: r.Reason})
			continue
		}
		if r.CWD != allowed || r.Sessions != 2 || r.Bytes == 0 || r.Newest.IsZero() || r.CWDHash == "" {
			t.Errorf("allowed row %+v", r)
		}
	}
	if !reflect.DeepEqual(fromRows, refused) {
		t.Fatalf("inventory's refused rows %+v, agent refused %+v", fromRows, refused)
	}
}

// Sync carries the inventory out whether the push went ahead, was
// refused whole, or failed at the lake.
func TestSyncCarriesTheInventory(t *testing.T) {
	work := t.TempDir()
	home := t.TempDir()
	tervaSession(t, home, "aaaa", "s1", work)
	opt := Options{TervaHome: home, MachineID: "m", StateDir: t.TempDir(), ServerURL: "http://127.0.0.1:1"}

	res, err := Sync(context.Background(), opt)
	if _, ok := err.(*Rejected); !ok {
		t.Fatalf("refused whole: %v", err)
	}
	if len(res.Inventory) != 1 || res.Inventory[0].Reason != config.RefusedEmptyAllow {
		t.Fatalf("refused inventory %+v", res.Inventory)
	}

	opt.Projects.Allow = []config.ProjectMatch{{CWDPrefix: work}}
	res, err = Sync(context.Background(), opt)
	if err == nil {
		t.Fatal("a lake that is not there took the push")
	}
	if len(res.Inventory) != 1 || res.Inventory[0].Reason != "" {
		t.Fatalf("inventory of a failed push %+v", res.Inventory)
	}

	res, _ = Sync(context.Background(), Options{MachineID: "m", StateDir: t.TempDir(), ServerURL: "http://127.0.0.1:1"})
	if res.Inventory == nil || len(res.Inventory) != 0 {
		t.Fatalf("an empty machine's inventory %#v", res.Inventory)
	}
}

func TestInventoryModes(t *testing.T) {
	at := time.Date(2026, 9, 28, 16, 0, 0, 0, time.FixedZone("x", 3600))
	rows := []InventoryRow{
		{CWD: "/w/app", CWDs: 2, GitRemote: "git@git.example:team/app.git", Harnesses: []string{"claude"}, Sessions: 3, Bytes: 30, Newest: at},
		{CWD: "/w/secret", CWDs: 1, Harnesses: []string{"codex"}, Sessions: 5, Bytes: 50, Reason: config.RefusedNoMatch},
		{CWD: "/w/deny", CWDs: 1, Harnesses: []string{"codex"}, Sessions: 1, Bytes: 7, Reason: config.RefusedByDeny},
	}
	soc := Inventory(protocol.InventorySociable, rows, at)
	if soc.Mode != "sociable" || !soc.GeneratedAt.Equal(at) || soc.GeneratedAt.Location() != time.UTC ||
		len(soc.Projects) != 3 || soc.RefusedSessions != 6 || soc.RefusedBytes != 57 {
		t.Fatalf("sociable %+v", soc)
	}
	if p := soc.Projects[0]; !p.Allowed || p.GitRemote != config.NormalizeRemote(rows[0].GitRemote) || p.CWDs != 2 {
		t.Fatalf("allowed project %+v", p)
	}
	if p := soc.Projects[1]; p.Allowed || p.Reason != config.RefusedNoMatch || p.CWD != "/w/secret" {
		t.Fatalf("refused project %+v", p)
	}
	strict := Inventory(protocol.InventoryStrict, rows, at)
	if len(strict.Projects) != 1 || !strict.Projects[0].Allowed || strict.RefusedSessions != 6 || strict.RefusedBytes != 57 {
		t.Fatalf("strict %+v", strict)
	}

	many := make([]InventoryRow, protocol.MaxInventoryProjects+1)
	for i := range many {
		many[i] = InventoryRow{CWD: "/p", Sessions: 1, Bytes: 1, Reason: config.RefusedNoMatch}
	}
	cut := Inventory(protocol.InventorySociable, many, at)
	if len(cut.Projects) != protocol.MaxInventoryProjects || !cut.Truncated || cut.RefusedSessions != len(many) {
		t.Fatalf("cut to %d, truncated %v, refused %d", len(cut.Projects), cut.Truncated, cut.RefusedSessions)
	}
}
