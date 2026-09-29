package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// withBays rewrites the fixture's config.json with bay requests for the
// work lake.
func (f *twoLakeFixture) withBays(t *testing.T, bays string) {
	t.Helper()
	path := filepath.Join(f.cfg, "terva-lampi", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	conf := strings.Replace(string(raw), `"projects": {"allow": [{"cwd_prefix": "/work/app"}]}`,
		`"projects": {"allow": [{"cwd_prefix": "/work/app"}]}, "bays": `+bays, 1)
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestBayRequestsRouteASessionToTwoBays is TKT-01M3NNF2CE: a rule and
// the lake route one session to two bays, and the lake places it only
// where this device may write.
func TestBayRequestsRouteASessionToTwoBays(t *testing.T) {
	ctx := t.Context()
	f := newTwoLakeFixture(t)
	f.withBays(t, `{"rules": [{"cwd_prefix": "/work/app", "bays": ["team", "billing", "secret"]}], "default": ["personal"]}`)
	devices, err := f.work.Catalog.Devices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, name := range []string{"team", "billing", "secret"} {
		b, err := f.work.Catalog.CreateBay(ctx, name, "admin", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = b.ID
		if name == "secret" {
			continue
		}
		for _, d := range devices {
			if _, err := f.work.Catalog.AddGrant(ctx, catalog.PrincipalDevice, d.ID, name, catalog.PermWrite, "admin", time.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(devices) == 0 {
		t.Fatal("the work lake has no device to grant")
	}
	if err := f.run("sync", "--lake", "work"); err != nil {
		t.Fatalf("sync: %v\n%s", err, f.stderr)
	}
	page, err := f.work.Catalog.DashboardSessions(ctx, catalog.AllBays(), catalog.PageRequest{Limit: 10})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("sessions %+v err=%v", page.Items, err)
	}
	got, err := f.work.Catalog.SessionBays(ctx, page.Items[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	want := []string{ids["billing"], ids["team"]}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bays %v want team and billing", got)
	}
}

func TestBaysWhich(t *testing.T) {
	f := newTwoLakeFixture(t)
	f.withBays(t, `{"rules": [{"cwd_prefix": "/work/app", "bays": ["team"]}, {"harness": "claude", "bays": ["agents"]}], "default": ["personal"]}`)
	if err := f.run("bays", "which", "/work/app/sub", "--harness", "claude"); err != nil {
		t.Fatal(err)
	}
	out := f.stdout.String()
	for _, want := range []string{
		"/work/app/sub",
		"default: does not upload: no allow rule matches",
		"work: asks for team, agents",
		"bays.rules[0] cwd_prefix=/work/app: team",
		"bays.rules[1] harness=claude: agents",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if err := f.run("bays", "which", "/work/app"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.stdout.String(), "agents") {
		t.Errorf("a harness rule matched without --harness:\n%s", f.stdout)
	}
	f = newTwoLakeFixture(t)
	f.withBays(t, `{"default": ["personal"]}`)
	if err := f.run("bays", "which", "/work/app"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "bays.default: personal") {
		t.Errorf("default not named:\n%s", f.stdout)
	}
	// A project the lake refuses still names the bays it would ask for,
	// so a rule can be checked first (review 1440).
	f.stdout.Reset()
	if err := f.run("bays", "which", "/elsewhere"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"work: does not upload: no allow rule matches", "would ask for personal", "bays.default: personal"} {
		if !strings.Contains(f.stdout.String(), want) {
			t.Errorf("refused project: missing %q in:\n%s", want, f.stdout)
		}
	}
	if err := f.run("bays", "nope"); err == nil {
		t.Error("unknown subcommand accepted")
	}
}
