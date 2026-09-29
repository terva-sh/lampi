package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
)

// adoptFixture is a lake with a token-file device, laptop, and a machine
// that syncs to it the legacy way: top-level server, the legacy token
// file holding laptop's token, and a local allowlist of /work. It has a
// session in /work/app, which the lake's default profile allows, and
// one in /work/other, which it does not.
type adoptFixture struct {
	t                *testing.T
	url              string
	lake             *api.Server
	home, cfg, state string
	stdout, stderr   bytes.Buffer
}

// laptopToken is the token registerLake gives device laptop.
var laptopToken = strings.Repeat("c3", 32)

func newAdoptFixture(t *testing.T) *adoptFixture {
	t.Helper()
	_, lake, url := registerLake(t)
	putDefaultProfile(t, lake, config.Profile{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}},
	}})
	home, cfg, state, _ := agentFixture(t, url)
	other := filepath.Join(home, "sessions", "efgh")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "sess-2.jsonl"), []byte("{\"type\":\"meta\",\"meta\":{\"id\":\"sess-2\",\"cwd\":\"/work/other\"}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An unknown key and a key under projects the lift does not own must
	// survive the rewrite.
	writeAgentConfig(t, cfg, `{"server":`+jsonString(url)+`,"projects":{"allow":[{"cwd_prefix":"/work"}],"deny":[{"cwd_prefix":"/work/secret"}]},"future_key":{"x":1},"agent":{"debounce":"100ms"}}`)
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "token"), []byte(laptopToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &adoptFixture{t: t, url: url, lake: lake, home: home, cfg: cfg, state: state}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (f *adoptFixture) run(args ...string) error {
	f.stdout.Reset()
	f.stderr.Reset()
	env := Env{Stdin: strings.NewReader(""), Stdout: &f.stdout, Stderr: &f.stderr, Getenv: agentGetenv(f.home, f.cfg, f.state)}
	return Run(args, env)
}

func (f *adoptFixture) fingerprint() string {
	return identity.Fingerprint(f.lake.Identity().Keys[0].Pub)
}

func (f *adoptFixture) configJSON() map[string]json.RawMessage {
	f.t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.cfg, "terva-lampi", "config.json"))
	if err != nil {
		f.t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		f.t.Fatal(err)
	}
	return top
}

func (f *adoptFixture) file() config.File {
	f.t.Helper()
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		f.t.Fatal(err)
	}
	return file
}

func TestLakesAdoptPinsTheLegacyLakeAndKeepsItsState(t *testing.T) {
	f := newAdoptFixture(t)
	if err := f.run("sync"); err != nil {
		t.Fatalf("sync: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "uploaded 2") {
		t.Fatalf("first sync:\n%s", f.stdout.String())
	}
	machine := readMachineID(t, f.cfg)
	before, _ := os.ReadFile(filepath.Join(f.cfg, "terva-lampi", "config.json"))

	// No terminal and no fingerprint, or the wrong one: nothing changes.
	if err := f.run("lakes", "adopt"); err == nil || !strings.Contains(err.Error(), "check 3, confirmation: stdin is not a terminal") {
		t.Fatalf("no fingerprint: %v", err)
	}
	if err := f.run("lakes", "adopt", "--fingerprint", "SHA256:nope"); err == nil || !strings.Contains(err.Error(), "check 3, the fingerprint") {
		t.Fatalf("wrong fingerprint: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(f.cfg, "terva-lampi", "config.json")); !bytes.Equal(before, after) {
		t.Fatalf("a refused adopt changed config.json:\n%s", after)
	}

	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("adopt: %v\n%s%s", err, f.stdout.String(), f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "lake default: pinned to lake "+f.lake.Identity().LakeID) ||
		!strings.Contains(f.stdout.String(), "the 1 local allow rules stay in force") {
		t.Fatalf("adopt output:\n%s", f.stdout.String())
	}
	top := f.configJSON()
	for _, k := range []string{"server", "token_file"} {
		if _, ok := top[k]; ok {
			t.Fatalf("top-level %s is still set: %s", k, top[k])
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, top["future_key"]); err != nil || compact.String() != `{"x":1}` {
		t.Fatalf("unknown key lost: %s", top["future_key"])
	}
	file := f.file()
	if len(file.Projects.Allow) != 0 || len(file.Projects.Deny) != 1 || file.Projects.Deny[0].CWDPrefix != "/work/secret" {
		t.Fatalf("top-level projects %+v", file.Projects)
	}
	lc := file.Lakes[config.DefaultLake]
	key := f.lake.Identity().Public()[0]
	if lc.Server != f.url || lc.LakeID != f.lake.Identity().LakeID || lc.KeyID != key.ID || lc.PublicKey != key.PublicKey || lc.TokenFile != "" {
		t.Fatalf("entry %+v", lc)
	}
	if len(lc.Projects.Allow) != 1 || lc.Projects.Allow[0].CWDPrefix != "/work" {
		t.Fatalf("local allow rules not kept under the entry: %+v", lc.Projects)
	}
	// The device id comes from the profile the lake signed for laptop.
	if lc.DeviceID == "" || !strings.Contains(f.stdout.String(), "device "+lc.DeviceID) {
		t.Fatalf("device id %q:\n%s", lc.DeviceID, f.stdout.String())
	}
	if _, err := os.Stat(filepath.Join(f.state, "terva-lampi", "lakes", "default", "profile.json")); err != nil {
		t.Fatalf("profile not cached: %v", err)
	}
	if got := readMachineID(t, f.cfg); got != machine {
		t.Fatalf("machine id changed: %s -> %s", machine, got)
	}

	// The same lake, token, machine and watermarks: nothing is sent again,
	// and the pinned hello passes.
	if err := f.run("sync"); err != nil {
		t.Fatalf("sync after adopt: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "uploaded 0") || !strings.Contains(f.stdout.String(), "manifests 0") {
		t.Fatalf("sync after adopt:\n%s%s", f.stdout.String(), f.stderr.String())
	}

	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "already pinned") {
		t.Fatalf("second adopt: %v", err)
	}
}

func TestLakesAdoptAllowFromProfileRefusesWhatItWouldDrop(t *testing.T) {
	f := newAdoptFixture(t)
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint(), "--allow-from", "profile"); err == nil || !strings.Contains(err.Error(), "would refuse 1 projects it uploads now") {
		t.Fatalf("narrowing adopt: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stderr.String(), "/work/other") || strings.Contains(f.stderr.String(), "/work/app") {
		t.Fatalf("listed:\n%s", f.stderr.String())
	}
	if _, ok := f.file().Lakes[config.DefaultLake]; ok {
		t.Fatal("a refused adopt wrote the entry")
	}

	// Pinned but keeping the local rules, then the switch alone.
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("adopt: %v\n%s", err, f.stderr.String())
	}
	if err := f.run("lakes", "adopt", "--allow-from", "profile"); err == nil || !strings.Contains(err.Error(), "would refuse 1 projects") {
		t.Fatalf("switch: %v", err)
	}
	if err := f.run("lakes", "adopt", "--allow-from", "profile", "--force"); err != nil {
		t.Fatalf("forced switch: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "removed 1 local allow rules; profile default allows 1") {
		t.Fatalf("switch output:\n%s", f.stdout.String())
	}
	if lc := f.file().Lakes[config.DefaultLake]; len(lc.Projects.Allow) != 0 || lc.LakeID == "" {
		t.Fatalf("entry after switch %+v", lc)
	}
	f.stdout.Reset()
	if err := f.run("agent", "config"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "allow_source=lake:default") {
		t.Fatalf("agent config:\n%s", f.stdout.String())
	}
}

func TestLakesAdoptSwitchesWhenTheProfileCoversEverything(t *testing.T) {
	f := newAdoptFixture(t)
	putDefaultProfile(t, f.lake, config.Profile{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work"}},
	}})
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint(), "--allow-from", "profile"); err != nil {
		t.Fatalf("adopt: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "removed 1 local allow rules; the profile's 1 apply") {
		t.Fatalf("output:\n%s", f.stdout.String())
	}
}

func TestLakesAdoptRefusesATokenTheLakeDoesNotKnow(t *testing.T) {
	f := newAdoptFixture(t)
	if err := os.WriteFile(filepath.Join(f.cfg, "terva-lampi", "token"), []byte(strings.Repeat("d4", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "check 2, hello with this machine's token") {
		t.Fatalf("unknown token: %v", err)
	}
	if err := os.Remove(filepath.Join(f.cfg, "terva-lampi", "token")); err != nil {
		t.Fatal(err)
	}
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "has no device token") {
		t.Fatalf("no token: %v", err)
	}
}

func TestLakesAdoptRefusesAProfileThatAllowsNothing(t *testing.T) {
	f := newAdoptFixture(t)
	putDefaultProfile(t, f.lake, config.Profile{})
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint(), "--allow-from", "profile"); err == nil || !strings.Contains(err.Error(), "allows no project") {
		t.Fatalf("empty profile: %v", err)
	}
}

// Review of #133: switching a pinned lake compares with the profile the
// agent applied, so a deny rule the lake added since is caught.
func TestLakesAdoptSwitchCatchesADenyAddedSinceTheAgentApplied(t *testing.T) {
	f := newAdoptFixture(t)
	putDefaultProfile(t, f.lake, config.Profile{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work"}},
	}})
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("adopt: %v\n%s", err, f.stderr.String())
	}
	putDefaultProfile(t, f.lake, config.Profile{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work"}},
		Deny:  []config.ProjectMatch{{CWDPrefix: "/work/app"}},
	}})
	if err := f.run("lakes", "adopt", "--allow-from", "profile"); err == nil || !strings.Contains(err.Error(), "would refuse 1 projects") {
		t.Fatalf("switch with a new deny: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stderr.String(), "/work/app") {
		t.Fatalf("listed:\n%s", f.stderr.String())
	}
}

// Review of #133: the write refuses an entry that changed after it was
// checked: another server, token, pin or allow rule.
func TestAdoptChangedSeesEveryCheckedField(t *testing.T) {
	l := config.Lake{
		Name:      config.DefaultLake,
		Server:    config.Setting{Value: "http://127.0.0.1:8787", Source: config.SourceConfig},
		TokenFile: config.Setting{Value: "/t", Source: config.SourceDefault},
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work"}}},
	}
	same := config.LakeConfig{Server: "http://127.0.0.1:8787", Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work"}}}}
	if adoptChanged(l, same) {
		t.Fatal("the checked entry reads as changed")
	}
	for name, edit := range map[string]func(*config.LakeConfig){
		"server":   func(lc *config.LakeConfig) { lc.Server = "http://127.0.0.1:9999" },
		"lake id":  func(lc *config.LakeConfig) { lc.LakeID = "lake_x" },
		"allow":    func(lc *config.LakeConfig) { lc.Projects.Allow = []config.ProjectMatch{{CWDPrefix: "/other"}} },
		"no allow": func(lc *config.LakeConfig) { lc.Projects.Allow = nil },
		"more allow": func(lc *config.LakeConfig) {
			lc.Projects.Allow = append(lc.Projects.Allow, config.ProjectMatch{CWDPrefix: "/more"})
		},
	} {
		lc := same
		lc.Projects.Allow = append([]config.ProjectMatch(nil), same.Projects.Allow...)
		edit(&lc)
		if !adoptChanged(l, lc) {
			t.Errorf("%s: a changed entry reads as the same", name)
		}
	}
}

// Review of #133: the switch on a pinned lake caches the profile it
// checked, so the agent that reloads applies what was approved, not an
// older cached copy.
func TestLakesAdoptSwitchCachesTheProfileItChecked(t *testing.T) {
	f := newAdoptFixture(t)
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("adopt: %v\n%s", err, f.stderr.String())
	}
	putDefaultProfile(t, f.lake, config.Profile{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: "/work"}},
	}})
	if err := f.run("lakes", "adopt", "--allow-from", "profile"); err != nil {
		t.Fatalf("switch: %v\n%s", err, f.stderr.String())
	}
	lakes, err := config.ResolveLakes(f.file(), agentGetenv(f.home, f.cfg, f.state), config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	d, ok, err := lakeprofile.Load(lakestate.Dir(filepath.Join(f.state, "terva-lampi"), config.DefaultLake), lakes[0])
	if err != nil || !ok {
		t.Fatalf("cached profile: %v %v", ok, err)
	}
	if a := d.Profile.Projects.Allow; len(a) != 1 || a[0].CWDPrefix != "/work" {
		t.Fatalf("cached profile is not the one checked: %+v", d.Profile.Projects)
	}
}

// Review of #133: the pin and device id were checked for one token; a
// token file that changed before the write refuses it.
func TestCommitAdoptRefusesATokenThatChanged(t *testing.T) {
	f := newAdoptFixture(t)
	before, _ := os.ReadFile(filepath.Join(f.cfg, "terva-lampi", "config.json"))
	env := Env{Stdout: &f.stdout, Stderr: &f.stderr, Getenv: agentGetenv(f.home, f.cfg, f.state)}
	lakes, err := config.ResolveLakes(f.file(), env.getenv, config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	err = commitAdopt(env, lakes[0], strings.Repeat("d4", 32), lakeprofile.Doc{}, func(lc *config.LakeConfig) { lc.LakeID = "lake_x" })
	if err == nil || !strings.Contains(err.Error(), "changed in config.json or its token file") {
		t.Fatalf("stale token: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(f.cfg, "terva-lampi", "config.json")); !bytes.Equal(before, after) {
		t.Fatalf("config.json changed:\n%s", after)
	}
}

// Review of #133: a profile's harness settings apply after adopting, so
// one that turns off a harness this machine reads is listed and refused.
func TestLakesAdoptRefusesAProfileThatTurnsOffAHarness(t *testing.T) {
	f := newAdoptFixture(t)
	putDefaultProfile(t, f.lake, config.Profile{
		Harnesses: config.Harnesses{"terva": {Enabled: false}},
		Projects:  config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work"}}},
	})
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "terva would stop being read") {
		t.Fatalf("adopt: %v\n%s", err, f.stderr.String())
	}
	// The projects that go with it are listed, though the project rules
	// do not change.
	if !strings.Contains(f.stderr.String(), "uploaded now from terva: 2 sessions in 2 projects") || !strings.Contains(f.stderr.String(), "/work/other") {
		t.Fatalf("listed:\n%s", f.stderr.String())
	}
	if _, ok := f.file().Lakes[config.DefaultLake]; ok {
		t.Fatal("a refused adopt wrote the entry")
	}
	// config.json wins over a profile, so setting the harness there keeps
	// it on.
	writeAgentConfig(t, f.cfg, `{"server":`+jsonString(f.url)+`,"harnesses":{"terva":{"enabled":true}},"projects":{"allow":[{"cwd_prefix":"/work"}]}}`)
	if err := f.run("lakes", "adopt", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("adopt with the harness set locally: %v\n%s", err, f.stderr.String())
	}
}
