package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/regcode"
)

// regFixture is a lake that can mint codes, with a profile that allows
// /work/app, and a client machine with a session and no config.
type regFixture struct {
	t                *testing.T
	lakeDir, url     string
	lake             *api.Server
	home, cfg, state string
	stdout, stderr   bytes.Buffer
}

func newRegFixture(t *testing.T) *regFixture {
	t.Helper()
	dir, lake, url := registerLake(t)
	lake.SetProfiles(config.Profiles{config.DefaultProfile: {Projects: config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}}}})
	if err := Run([]string{"serve", "identity", "set-url", url, "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	home, cfg, state, _ := agentFixture(t, url)
	// A fresh machine: no config.json.
	if err := os.Remove(filepath.Join(cfg, "terva-lampi", "config.json")); err != nil {
		t.Fatal(err)
	}
	return &regFixture{t: t, lakeDir: dir, url: url, lake: lake, home: home, cfg: cfg, state: state}
}

func (f *regFixture) mint(name string) string {
	f.t.Helper()
	var out bytes.Buffer
	if err := Run([]string{"serve", "register", "--name", name, "--data", f.lakeDir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(out.String())
}

func (f *regFixture) fingerprint() string {
	return identity.Fingerprint(f.lake.Identity().Keys[0].Pub)
}

func (f *regFixture) env(stdin string) Env {
	return Env{Stdin: strings.NewReader(stdin), Stdout: &f.stdout, Stderr: &f.stderr, Getenv: agentGetenv(f.home, f.cfg, f.state)}
}

func (f *regFixture) register(stdin string, args ...string) error {
	f.stdout.Reset()
	f.stderr.Reset()
	return Run(append([]string{"register"}, args...), f.env(stdin))
}

func TestRegisterOnAFreshMachineThenSync(t *testing.T) {
	f := newRegFixture(t)
	code := f.mint("newbox")
	if err := f.register(code+"\n", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatalf("%v\n%s%s", err, f.stdout.String(), f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "registered as device newbox (dev_") || !strings.Contains(f.stderr.String(), f.fingerprint()) {
		t.Fatalf("output:\n%s%s", f.stdout.String(), f.stderr.String())
	}
	tokenPath := filepath.Join(f.cfg, "terva-lampi", "tokens", "default.token")
	raw, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(raw))
	if st, _ := os.Stat(tokenPath); st.Mode().Perm() != 0o600 {
		t.Fatalf("token mode %v", st.Mode())
	}
	if strings.Contains(f.stdout.String()+f.stderr.String(), token) {
		t.Fatal("register printed the token")
	}
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		t.Fatal(err)
	}
	lc := file.Lakes["default"]
	if lc.Server != f.url || lc.LakeID != f.lake.Identity().LakeID || lc.KeyID == "" || lc.PublicKey == "" || lc.TokenFile != tokenPath {
		t.Fatalf("lake entry %+v", lc)
	}
	// The entry names the device the lake made, so only profiles signed
	// for it verify; the sync below reads the cached one that way.
	if !strings.HasPrefix(lc.DeviceID, "dev_") || !strings.Contains(f.stdout.String(), "("+lc.DeviceID+")") {
		t.Fatalf("device id %q, output:\n%s", lc.DeviceID, f.stdout.String())
	}
	if _, err := os.Stat(filepath.Join(f.state, "terva-lampi", "lakes", "default", "profile.json")); err != nil {
		t.Fatalf("base configuration not stored: %v", err)
	}
	// The machine has everything it needs: the allowlist came from the
	// lake's profile.
	f.stdout.Reset()
	if err := Run([]string{"sync"}, f.env("")); err != nil {
		t.Fatalf("sync: %v\n%s", err, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "uploaded 1") {
		t.Fatalf("sync:\n%s", f.stdout.String())
	}

	// A second code for the same lake needs --replace.
	again := f.mint("newbox-2")
	if err := f.register(again+"\n", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "lake default in config.json is already this lake") {
		t.Fatalf("duplicate: %v", err)
	}
	// The old device still holds this machine until the operator revokes
	// it.
	if err := f.register(again+"\n", "--fingerprint", f.fingerprint(), "--replace"); err == nil || !strings.Contains(err.Error(), "revokes or unbinds") {
		t.Fatalf("replace before revoke: %v", err)
	}
	if _, err := f.lake.Catalog.RevokeDevice(t.Context(), "newbox", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.register(again+"\n", "--fingerprint", f.fingerprint(), "--replace"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	raw2, _ := os.ReadFile(tokenPath)
	if strings.TrimSpace(string(raw2)) == token {
		t.Fatal("--replace kept the old token")
	}
}

func TestRegisterRefusesEachCheck(t *testing.T) {
	f := newRegFixture(t)
	now := time.Now()
	good := f.mint("box")
	c, err := regcode.Decode(good)
	if err != nil {
		t.Fatal(err)
	}
	other, err := identity.New(rand.Reader, now)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(id *identity.Identity, c regcode.Code) string {
		s, err := regcode.Encode(id, c, now)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	// A tampered code: one character of the payload changed.
	i := len(regcode.Prefix) + 10
	tampered := good[:i] + string("AB"[boolIndex(good[i] == 'A')]) + good[i+1:]
	cases := []struct {
		name, code string
		args       []string
		want       string
	}{
		{"tampered", tampered, nil, "check 1"},
		{"expired", encode(f.lake.Identity(), regcode.Code{URL: f.url, Secret: c.Secret, Expires: now.Add(-time.Hour)}), nil, "check 1, the code: it expired"},
		{"plain http", encode(f.lake.Identity(), regcode.Code{URL: "http://lake.example", Secret: c.Secret, Expires: now.Add(time.Hour)}), nil, "check 2"},
		{"another key", encode(other, regcode.Code{URL: f.url, Secret: c.Secret, Expires: now.Add(time.Hour)}), nil, "check 3"},
		{"wrong fingerprint", good, []string{"--fingerprint", identity.Fingerprint(other.Keys[0].Pub)}, "check 4, the fingerprint"},
		{"no terminal", good, []string{}, "check 4, confirmation: stdin is not a terminal"},
	}
	for _, tc := range cases {
		args := tc.args
		if args == nil {
			args = []string{"--fingerprint", f.fingerprint()}
		}
		if err := f.register(tc.code+"\n", args...); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	if err := f.register("", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "no code on stdin") {
		t.Fatalf("empty stdin: %v", err)
	}
	if err := f.register("", good); err == nil || !strings.Contains(err.Error(), "not take the code as an argument") {
		t.Fatalf("code as argument: %v", err)
	}
	// Nothing was registered or written, and the good code still works.
	if _, err := os.Stat(filepath.Join(f.cfg, "terva-lampi", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused register wrote config.json: %v", err)
	}
	codeFile := filepath.Join(t.TempDir(), "code")
	if err := os.WriteFile(codeFile, []byte(good+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.register("", "--code-file", codeFile, "--fingerprint", strings.TrimPrefix(f.fingerprint(), "SHA256:"), "--lake", "work"); err != nil {
		t.Fatalf("code file: %v\n%s", err, f.stderr.String())
	}
	regs, _ := f.lake.Catalog.Registrations(t.Context())
	used := 0
	for _, r := range regs {
		if r.State(time.Now()) == "used" {
			used++
		}
	}
	if used != 1 {
		t.Fatalf("used codes %d", used)
	}
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestRegisterRefusesALakeWithNoKeyEndpoint(t *testing.T) {
	f := newRegFixture(t)
	code := f.mint("box")
	fp := f.fingerprint()
	f.lake.SetIdentity(nil)
	if err := f.register(code+"\n", "--fingerprint", fp); err == nil || !strings.Contains(err.Error(), "upgrade the lake") {
		t.Fatalf("%v", err)
	}
}

func TestRegisterAsksOnATerminal(t *testing.T) {
	f := newRegFixture(t)
	old := terminalStdin
	terminalStdin = func(Env) bool { return true }
	defer func() { terminalStdin = old }()
	if err := f.register(f.mint("box") + "\nn\n"); err == nil || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("declined: %v", err)
	}
	if !strings.Contains(f.stderr.String(), "Paste the registration code") || !strings.Contains(f.stderr.String(), "[y/N]") {
		t.Fatalf("prompts:\n%s", f.stderr.String())
	}
	if err := f.register(f.mint("box2") + "\nyes\n"); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
}

func TestLakesListAndRemove(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"sync"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	f.stdout.Reset()
	if err := Run([]string{"lakes"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.stdout.String(), "lake work server="+f.url) || !strings.Contains(f.stdout.String(), "lake work profile=default") {
		t.Fatalf("list:\n%s", f.stdout.String())
	}
	stateDir := filepath.Join(f.state, "terva-lampi", "lakes", "work")
	f.stdout.Reset()
	if err := Run([]string{"lakes", "remove", "work"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); err != nil {
		t.Fatal("remove dropped the state without --purge-state")
	}
	if _, err := os.Stat(filepath.Join(f.cfg, "terva-lampi", "tokens", "work.token")); !os.IsNotExist(err) {
		t.Fatal("token file kept")
	}
	if !strings.Contains(f.stdout.String(), "serve devices revoke") {
		t.Fatalf("remove output:\n%s", f.stdout.String())
	}
	f.stdout.Reset()
	if err := Run([]string{"lakes"}, f.env("")); err != nil || !strings.Contains(f.stdout.String(), "lakes: none configured") {
		t.Fatalf("after remove: %v\n%s", err, f.stdout.String())
	}
	// A running agent keeps the state; without one it goes.
	release, err := writeAgentPID(filepath.Join(f.state, "terva-lampi"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"lakes", "remove", "work", "--purge-state"}, f.env("")); err == nil || !strings.Contains(err.Error(), "an agent is running") {
		t.Fatalf("purge beside an agent: %v", err)
	}
	release()
	if err := Run([]string{"lakes", "remove", "work", "--purge-state"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal("--purge-state kept the state")
	}
	if err := Run([]string{"lakes", "remove", "nope"}, f.env("")); err == nil {
		t.Fatal("removed an unknown lake")
	}
	_ = catalog.ErrNoDevice
}

func TestRegisterInstallsTheUserService(t *testing.T) {
	f := newRegFixture(t)
	var calls []string
	oldRun, oldOS := runCommand, serviceGOOS
	runCommand = func(name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch {
		case name == "loginctl":
			return []byte("Linger=no\n"), nil
		case len(args) > 1 && args[1] == "show":
			// The user manager searches the default config directory,
			// while register runs with f.cfg (TKT-01M3GAHSQ).
			return []byte(unitPath(f.home) + "\n"), nil
		}
		return nil, nil
	}
	serviceGOOS = "linux"
	defer func() { runCommand, serviceGOOS = oldRun, oldOS }()
	getenv := agentGetenv(f.home, f.cfg, f.state)
	env := f.env(f.mint("box") + "\n")
	env.Getenv = func(k string) string {
		if k == "USER" {
			return "drew"
		}
		return getenv(k)
	}
	if err := Run([]string{"register", "--fingerprint", f.fingerprint(), "--install-service"}, env); err != nil {
		t.Fatal(err)
	}
	// Where the manager looks, not under register's own XDG_CONFIG_HOME.
	unit, err := os.ReadFile(filepath.Join(f.home, ".config", "systemd", "user", "terva-lampi-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.cfg, "systemd")); !os.IsNotExist(err) {
		t.Fatalf("a unit was also written under register's XDG_CONFIG_HOME: %v", err)
	}
	if !strings.Contains(string(unit), " agent\n") || !strings.Contains(string(unit), "ExecReload=/bin/kill -HUP $MAINPID") ||
		!strings.Contains(string(unit), "Environment=\"XDG_CONFIG_HOME="+f.cfg+"\"") {
		t.Fatalf("unit:\n%s", unit)
	}
	want := []string{"systemctl --user show --property=UnitPath --value", "systemctl --user daemon-reload", "systemctl --user enable --now terva-lampi-agent.service", "loginctl show-user drew --property=Linger"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %q", calls)
	}
	if !strings.Contains(f.stdout.String(), "loginctl enable-linger drew") {
		t.Fatalf("no linger hint:\n%s", f.stdout.String())
	}
}

func TestLakesRemovePurgeBesideAnAgentChangesNothing(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(f.cfg, "terva-lampi", "tokens", "work.token")
	release, err := writeAgentPID(filepath.Join(f.state, "terva-lampi"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"lakes", "remove", "work", "--purge-state"}, f.env("")); err == nil || !strings.Contains(err.Error(), "nothing was removed") {
		t.Fatalf("purge beside an agent: %v", err)
	}
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Lakes["work"]; !ok {
		t.Fatal("a refused purge removed the lake from config.json")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("a refused purge removed the token: %v", err)
	}
	release()
	f.stdout.Reset()
	if err := Run([]string{"lakes", "remove", "work", "--purge-state"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatal("token file kept")
	}
	if !strings.Contains(f.stdout.String(), "no agent is running") || !strings.Contains(f.stdout.String(), "removed sync state") {
		t.Fatalf("remove output:\n%s", f.stdout.String())
	}
}

func TestLakesRemovePurgeRefusesANameThatIsNotALake(t *testing.T) {
	f := newRegFixture(t)
	lakes := filepath.Join(f.state, "terva-lampi", "lakes")
	if err := os.MkdirAll(filepath.Join(lakes, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	// ".." resolves to the state directory itself, which exists.
	for _, name := range []string{"..", ".", "work/..", "Work"} {
		if err := Run([]string{"lakes", "remove", name, "--purge-state"}, f.env("")); err == nil || !strings.Contains(err.Error(), "is not a lake name") {
			t.Fatalf("remove %q: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(lakes, "work")); err != nil {
		t.Fatalf("a refused name removed another lake's state: %v", err)
	}
}

func TestRegisterReplaceNeverTakesOverAnotherLakesEntry(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	getenv := agentGetenv(f.home, f.cfg, f.state)
	before, err := config.LoadFile(getenv)
	if err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(f.cfg, "terva-lampi", "tokens", "work.token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}

	// A second lake's code, pointed at the first lake's entry.
	dir, other, url := registerLake(t)
	if err := Run([]string{"serve", "identity", "set-url", url, "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run([]string{"serve", "register", "--name", "box", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	fp := identity.Fingerprint(other.Identity().Keys[0].Pub)
	if err := f.register(strings.TrimSpace(out.String())+"\n", "--fingerprint", fp, "--lake", "work", "--replace"); err == nil || !strings.Contains(err.Error(), "configured for another lake") {
		t.Fatalf("replace over another lake: %v", err)
	}
	after, err := config.LoadFile(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Lakes, before.Lakes) {
		t.Fatalf("lakes changed: %+v", after.Lakes)
	}
	if now, _ := os.ReadFile(tokenPath); string(now) != string(token) {
		t.Fatal("the first lake's token was overwritten")
	}
	regs, _ := other.Catalog.Registrations(t.Context())
	for _, r := range regs {
		if r.State(time.Now()) == "used" {
			t.Fatal("the refused code was redeemed")
		}
	}
}

func TestRegisterReplaceKeepsTheExistingName(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	if err := f.register(f.mint("box-2")+"\n", "--fingerprint", f.fingerprint(), "--lake", "home", "--replace"); err == nil || !strings.Contains(err.Error(), "lake work in config.json is already this lake") {
		t.Fatalf("replace under a second name: %v", err)
	}
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Lakes["home"]; ok || len(file.Lakes) != 1 {
		t.Fatalf("lakes %v", file.Lakes)
	}
}

func TestRegisterReplaceLeavesTheReplacedEntrysTokenFileAlone(t *testing.T) {
	f := newRegFixture(t)
	getenv := agentGetenv(f.home, f.cfg, f.state)
	// An entry for this server with no pinned lake id, naming a token
	// file placed by hand that another lake may share.
	shared := filepath.Join(t.TempDir(), "shared.token")
	if err := os.WriteFile(shared, []byte("shared\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetLake(getenv, "work", config.LakeConfig{Server: f.url, TokenFile: shared}); err != nil {
		t.Fatal(err)
	}
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work", "--replace"); err != nil {
		t.Fatalf("%v\n%s%s", err, f.stdout.String(), f.stderr.String())
	}
	if raw, _ := os.ReadFile(shared); string(raw) != "shared\n" {
		t.Fatal("--replace overwrote the token file the replaced entry named")
	}
	tokenPath := filepath.Join(f.cfg, "terva-lampi", "tokens", "work.token")
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("new token not in its own file: %v", err)
	}
	file, err := config.LoadFile(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if lc := file.Lakes["work"]; lc.TokenFile != tokenPath || lc.LakeID != f.lake.Identity().LakeID {
		t.Fatalf("lake entry %+v", lc)
	}
}

func TestLakesRemoveFailsWhenTheTokenCannotBeRemoved(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory in the token's place makes the removal fail
	// for a reason other than absence, on any platform and as any user.
	tokenPath := filepath.Join(f.cfg, "terva-lampi", "tokens", "work.token")
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tokenPath, "keep"), 0o700); err != nil {
		t.Fatal(err)
	}
	f.stdout.Reset()
	err := Run([]string{"lakes", "remove", "work"}, f.env(""))
	if err == nil || !strings.Contains(err.Error(), "its token is still in "+tokenPath) {
		t.Fatalf("remove with a stuck token: %v\n%s", err, f.stdout.String())
	}
	// The rest of the removal still happened.
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := file.Lakes["work"]; ok {
		t.Fatal("the lake is still in config.json")
	}
	if strings.Contains(f.stdout.String(), "removed "+tokenPath) {
		t.Fatalf("remove reported the token removed:\n%s", f.stdout.String())
	}

	// A token already gone is not a failure.
	if err := config.SetLake(agentGetenv(f.home, f.cfg, f.state), "home", config.LakeConfig{Server: f.url}); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"lakes", "remove", "home"}, f.env("")); err != nil {
		t.Fatalf("remove with no token file: %v", err)
	}
}

func TestChosenLakeNamesStayValidWithASuffix(t *testing.T) {
	long := strings.Repeat("a", 32)
	file := config.File{Server: "https://x.example", Lakes: map[string]config.LakeConfig{long: {}}}
	for range 10 {
		name := chooseLakeName(file, []config.Lake{{Name: config.DefaultLake}}, "https://"+long+".example")
		if !config.ValidLakeName(name) {
			t.Fatalf("chose %q, not a lake name", name)
		}
		if _, ok := file.Lakes[name]; ok {
			t.Fatalf("chose %q, which is taken", name)
		}
		file.Lakes[name] = config.LakeConfig{}
	}
}

func TestRegisterRefusesATokenFileAnotherLakeNames(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	getenv := agentGetenv(f.home, f.cfg, f.state)
	tokens := filepath.Join(f.cfg, "terva-lampi", "tokens")
	tokenPath := filepath.Join(tokens, "work.token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	// Another lake names the same file, spelled so that only a cleaned
	// path matches it.
	sep := string(filepath.Separator)
	unclean := tokens + sep + ".." + sep + "tokens" + sep + "work.token"
	if err := config.SetLake(getenv, "home", config.LakeConfig{Server: "https://home.invalid", TokenFile: unclean}); err != nil {
		t.Fatal(err)
	}
	err = f.register(f.mint("box-2")+"\n", "--fingerprint", f.fingerprint(), "--replace")
	if err == nil || !strings.Contains(err.Error(), "token_file of lake home") {
		t.Fatalf("replace over a shared token: %v", err)
	}
	if now, _ := os.ReadFile(tokenPath); string(now) != string(token) {
		t.Fatal("--replace overwrote a token another lake names")
	}
	regs, _ := f.lake.Catalog.Registrations(t.Context())
	used := 0
	for _, r := range regs {
		if r.State(time.Now()) == "used" {
			used++
		}
	}
	if used != 1 {
		t.Fatalf("%d codes redeemed; the refused one must not be", used)
	}
}

func TestLakesRemoveKeepsATokenAnotherLakeNames(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint(), "--lake", "work"); err != nil {
		t.Fatal(err)
	}
	getenv := agentGetenv(f.home, f.cfg, f.state)
	tokens := filepath.Join(f.cfg, "terva-lampi", "tokens")
	sep := string(filepath.Separator)
	if err := config.SetLake(getenv, "home", config.LakeConfig{Server: "https://home.invalid", TokenFile: tokens + sep + "." + sep + "work.token"}); err != nil {
		t.Fatal(err)
	}
	f.stdout.Reset()
	if err := Run([]string{"lakes", "remove", "work"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tokens, "work.token")); err != nil {
		t.Fatalf("remove deleted a token another lake names: %v", err)
	}
	if !strings.Contains(f.stdout.String(), "which lake home also names as its token_file") {
		t.Fatalf("remove output:\n%s", f.stdout.String())
	}

	// An entry with no token_file uses tokens/<name>.token, which
	// another lake can name too.
	spare := filepath.Join(tokens, "spare.token")
	if err := os.WriteFile(spare, []byte("spare\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := config.SetLake(getenv, "spare", config.LakeConfig{Server: "https://spare.invalid"}); err != nil {
		t.Fatal(err)
	}
	if err := config.SetLake(getenv, "home", config.LakeConfig{Server: "https://home.invalid", TokenFile: spare}); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"lakes", "remove", "spare"}, f.env("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(spare); err != nil {
		t.Fatalf("remove deleted a token another lake names: %v", err)
	}
}

func TestInstalledServicesKeepCustomXDGDirectories(t *testing.T) {
	oldRun := runCommand
	home := t.TempDir()
	runCommand = func(name string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "show" {
			return []byte(unitPath(home) + "\n"), nil
		}
		return nil, nil
	}
	defer func() { runCommand = oldRun }()
	cfg := t.TempDir()
	// A state directory whose name needs quoting in both files.
	state := filepath.Join(t.TempDir(), `50% "odd" \ & <dir>`)
	vars := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": cfg,
		"XDG_STATE_HOME":  state,
		// At its default, so left out.
		"XDG_DATA_HOME": filepath.Join(home, ".local", "share"),
	}
	env := Env{Stdout: ioDiscard(), Stderr: ioDiscard(), Getenv: func(k string) string { return vars[k] }}
	if err := installSystemd(env, "/opt/terva-lampi", true); err != nil {
		t.Fatal(err)
	}
	unit, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", systemdUnitName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"\nEnvironment=\"XDG_CONFIG_HOME=" + cfg + "\"\n",
		"\nEnvironment=\"XDG_STATE_HOME=" + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(state) + "\"\n",
	} {
		if !strings.Contains(string(unit), want) {
			t.Fatalf("unit lacks %q:\n%s", want, unit)
		}
	}
	if strings.Contains(string(unit), "XDG_DATA_HOME") {
		t.Fatalf("unit sets a directory at its default:\n%s", unit)
	}
	if err := installLaunchd(env, "/opt/terva-lampi", true); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	want := "<key>EnvironmentVariables</key>\n\t<dict>\n\t\t<key>XDG_CONFIG_HOME</key>\n\t\t<string>" + cfg + "</string>\n\t\t<key>XDG_STATE_HOME</key>\n\t\t<string>" + xmlEscape(state) + "</string>\n\t</dict>\n"
	if !strings.Contains(string(plist), want) {
		t.Fatalf("plist lacks the environment:\n%s", plist)
	}
	dec := xml.NewDecoder(bytes.NewReader(plist))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("plist is not well-formed: %v\n%s", err, plist)
		}
	}

	// With the directories unset, neither file sets any.
	home = t.TempDir()
	vars = map[string]string{"HOME": home}
	if err := installSystemd(env, "/opt/terva-lampi", true); err != nil {
		t.Fatal(err)
	}
	if err := installLaunchd(env, "/opt/terva-lampi", true); err != nil {
		t.Fatal(err)
	}
	unit, _ = os.ReadFile(filepath.Join(home, ".config", "systemd", "user", systemdUnitName))
	plist, _ = os.ReadFile(filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"))
	if !strings.Contains(string(unit), "ExecStart=") || strings.Contains(string(unit), "\nEnvironment=") {
		t.Fatalf("unit:\n%s", unit)
	}
	if !strings.Contains(string(plist), "ProgramArguments") || strings.Contains(string(plist), "EnvironmentVariables") {
		t.Fatalf("plist:\n%s", plist)
	}
}

// unitPath is a user manager's UnitPath with its config directory under
// home, in the order systemd lists it.
func unitPath(home string) string {
	return strings.Join([]string{
		home + "/.config/systemd/user.control", "/run/user/1000/systemd/user.control",
		"/run/user/1000/systemd/transient", home + "/.config/systemd/user", "/etc/systemd/user",
		"/run/user/1000/systemd/user", home + "/.local/share/systemd/user", "/usr/lib/systemd/user",
	}, " ")
}

// TKT-01M3GAHSQ: the unit goes where the running user manager looks.
func TestSystemdUserDirFollowsTheManager(t *testing.T) {
	oldRun := runCommand
	defer func() { runCommand = oldRun }()
	env := Env{Getenv: func(k string) string {
		return map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": "/tmp/register-only"}[k]
	}}
	cases := []struct {
		name string
		out  string
		err  error
		want string
	}{
		{"manager at the default", unitPath("/home/me"), nil, "/home/me/.config/systemd/user"},
		{"manager with its own config dir", strings.ReplaceAll(unitPath("/home/me"), "/home/me/.config", "/srv/cfg"), nil, "/srv/cfg/systemd/user"},
		{"config dir under /run", strings.ReplaceAll(unitPath("/home/me"), "/home/me/.config", "/run/cfg"), nil, "/run/cfg/systemd/user"},
		{"config dir with a space", strings.ReplaceAll(unitPath("/home/me"), "/home/me/.config", "/srv/my config"), nil, "/srv/my config/systemd/user"},
		{"SYSTEMD_UNIT_PATH first", "/opt/units " + unitPath("/home/me"), nil, "/home/me/.config/systemd/user"},
		{"no control directory", "/etc/systemd/user /usr/lib/systemd/user", nil, "/home/me/.config/systemd/user"},
		{"manager unreachable", "", errors.New("no bus"), "/home/me/.config/systemd/user"},
	}
	for _, c := range cases {
		runCommand = func(string, ...string) ([]byte, error) { return []byte(c.out), c.err }
		got, err := systemdUserDir(env)
		if err != nil || got != c.want {
			t.Errorf("%s: %q %v, want %q", c.name, got, err, c.want)
		}
	}
}
