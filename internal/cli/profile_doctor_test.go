package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
	"terva.sh/lampi/internal/protocol"
)

// serve profiles doctor reads the profile against a device's inventory,
// prints the findings, and --write applies only the safe fixes.
func TestServeProfilesDoctor(t *testing.T) {
	dir, run := profilesLake(t)
	ctx := t.Context()
	now := time.Now()
	cat, err := catalog.Open(filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	doc := `{"projects":{"allow":[{"cwd_prefix":"/work"},{"cwd_prefix":"/work/app"},{"cwd_prefix":"/home/steamos"},{"cwd_prefix":"/tmp/gone-1"}]}}`
	if _, _, err := cat.PutProfile(ctx, config.DefaultProfile, []byte(doc), "test", "", now); err != nil {
		t.Fatal(err)
	}
	ds, err := cat.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "deck"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{CWD: "/work/app", CWDs: 1, Sessions: 1, Allowed: true},
		{CWD: "/home/steamos/games", CWDs: 1, Sessions: 3, Allowed: true},
	}}
	if _, err := cat.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cat.Close()

	out, err := run("", "doctor", "default")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"profile default: 4 of 500 allow rules, 0 deny rules; 0 projects wait in review",
		"devices: deck",
		"problem  home_folder     allow[2] cwd_prefix /home/steamos",
		"fix: replace it with cwd_prefix /home/steamos/games",
		"warning  broad_folder    allow[0] cwd_prefix /work",
		"tidy     covered         allow[1] cwd_prefix /work/app",
		"tidy     temporary       allow[3] cwd_prefix /tmp/gone-1",
		"2 safe fixes change no project's verdict",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, out)
		}
	}

	file := filepath.Join(t.TempDir(), "fixed.json")
	if out, err = run("", "doctor", "default", "--write", file); err != nil || !strings.Contains(out, "wrote "+file+" with 2 safe fixes applied") {
		t.Fatalf("--write: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	p, err := config.ParseProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := []config.ProjectMatch{{CWDPrefix: "/work"}, {CWDPrefix: "/home/steamos"}}; !slices.Equal(p.Projects.Allow, want) {
		t.Errorf("written allow %+v, want %+v", p.Projects.Allow, want)
	}

	out, err = run("", "doctor", "default", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rep profiledoctor.Report
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Allow != 4 || len(rep.Findings) != 4 {
		t.Errorf("--json: %v %+v", err, rep)
	}
	if cur, _ := run("", "show", "default"); !strings.Contains(cur, "/tmp/gone-1") {
		t.Error("doctor changed the stored profile")
	}
	if _, err := run("", "doctor", "nosuch"); err == nil || !strings.Contains(err.Error(), "no profile named nosuch") {
		t.Errorf("unknown profile: %v", err)
	}
}
