package cli

import (
	"bytes"
	"crypto/rand"
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
		if name == "loginctl" {
			return []byte("Linger=no\n"), nil
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
	unit, err := os.ReadFile(filepath.Join(f.cfg, "systemd", "user", "terva-lampi-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), " agent\n") || !strings.Contains(string(unit), "ExecReload=/bin/kill -HUP $MAINPID") {
		t.Fatalf("unit:\n%s", unit)
	}
	want := []string{"systemctl --user daemon-reload", "systemctl --user enable --now terva-lampi-agent.service", "loginctl show-user drew --property=Linger"}
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
