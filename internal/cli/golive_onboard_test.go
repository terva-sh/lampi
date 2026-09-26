//go:build golive

package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/regcode"
	"terva.sh/lampi/internal/testharness"
)

// These drills run each lake as a real serve subprocess with a token
// file and a profiles file, and each client with closed XDG
// directories and explicit harness roots. No production host, no real
// credentials, and no real harness home is read.

type onboardLake struct {
	t      *testing.T
	f      *goLiveFixture
	name   string
	data   string
	tokens string
	url    string
	cmd    *exec.Cmd
}

// newOnboardLake starts a lake whose default profile allows allow.
func newOnboardLake(t *testing.T, f *goLiveFixture, name string, allow ...string) *onboardLake {
	t.Helper()
	l := &onboardLake{t: t, f: f, name: name, data: filepath.Join(f.root, "lake-"+name), tokens: filepath.Join(f.root, "tokens-"+name)}
	for _, dir := range []string{l.data, l.tokens} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// Registration needs a lake that requires tokens.
	operator, err := auth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.tokens, "operator.token"), []byte(operator+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var rules []map[string]string
	for _, p := range allow {
		rules = append(rules, map[string]string{"cwd_prefix": p})
	}
	profiles, _ := json.Marshal(map[string]any{"profiles": map[string]any{"default": map[string]any{"projects": map[string]any{"allow": rules}}}})
	if err := os.WriteFile(filepath.Join(l.data, "profiles.json"), profiles, 0o600); err != nil {
		t.Fatal(err)
	}
	l.start()
	if _, errs, err := f.run("serve", "identity", "set-url", l.url, "--data", l.data); err != nil {
		t.Fatalf("set-url: %v %s", err, errs)
	}
	return l
}

func (l *onboardLake) start() {
	l.url, l.cmd = goLiveServeArgs(l.t, l.data, "--token-file", l.tokens)
}

func (l *onboardLake) stop() {
	_ = l.cmd.Process.Kill()
	_ = l.cmd.Wait()
}

var fingerprintLine = regexp.MustCompile(`(?m)^key \S+ active (SHA256:\S+)`)

func (l *onboardLake) fingerprint() string {
	l.t.Helper()
	out, _, err := l.f.run("serve", "identity", "--data", l.data)
	if err != nil {
		l.t.Fatal(err)
	}
	m := fingerprintLine.FindStringSubmatch(out)
	if m == nil {
		l.t.Fatalf("no active key:\n%s", out)
	}
	return m[1]
}

func (l *onboardLake) mint(device string, args ...string) string {
	l.t.Helper()
	out, errs, err := l.f.run(append([]string{"serve", "register", "--name", device, "--data", l.data}, args...)...)
	if err != nil {
		l.t.Fatalf("mint: %v %s", err, errs)
	}
	return strings.TrimSpace(out)
}

func (l *onboardLake) devices() string {
	l.t.Helper()
	out, _, err := l.f.run("serve", "devices", "--data", l.data)
	if err != nil {
		l.t.Fatal(err)
	}
	return out
}

// onboardClient writes a client config.json with the fixture's harness
// roots, local deny rules, and "lakes": {} when standalone is set, or no
// lakes key at all.
func onboardClient(t *testing.T, f *goLiveFixture, standalone bool, deny ...string) {
	t.Helper()
	harnesses := map[string]any{}
	for id, home := range f.homes {
		harnesses[id] = map[string]any{"root": home, "enabled": id != "cursor" && id != "cursor-cli"}
	}
	cfg := map[string]any{"harnesses": harnesses, "agent": map[string]any{"debounce": "100ms", "debounce_max": "1s"}}
	var rules []map[string]string
	for _, p := range deny {
		rules = append(rules, map[string]string{"cwd_prefix": p})
	}
	if len(rules) > 0 {
		cfg["projects"] = map[string]any{"deny": rules}
	}
	if standalone {
		cfg["lakes"] = map[string]any{}
	}
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(f.root, "config", "terva-lampi", "config.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *goLiveFixture) runStdin(stdin string, args ...string) (string, string, error) {
	var out, errs bytes.Buffer
	err := Run(args, Env{Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errs, Getenv: f.getenv})
	return out.String(), errs.String(), err
}

func (f *goLiveFixture) register(l *onboardLake, code string, args ...string) error {
	_, errs, err := f.runStdin(code+"\n", append([]string{"register", "--fingerprint", l.fingerprint()}, args...)...)
	if err != nil {
		return fmt.Errorf("%w: %s", err, errs)
	}
	return nil
}

func plantAt(t *testing.T, f *goLiveFixture, cwd string, ids ...string) {
	t.Helper()
	var specs []testharness.SessionSpec
	for _, id := range ids {
		specs = append(specs, testharness.SessionSpec{ID: id})
	}
	if _, err := testharness.PlantTerva(f.homes["terva"], cwd, specs); err != nil {
		t.Fatal(err)
	}
}

func sessionsOn(t *testing.T, f *goLiveFixture, lake string) string {
	t.Helper()
	out, errs, err := f.run("status", "--lake", lake)
	if err != nil {
		t.Fatalf("status %s: %v %s", lake, err, errs)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "catalog_sessions: ") {
			return strings.TrimPrefix(line, "catalog_sessions: ")
		}
	}
	t.Fatalf("no catalog_sessions for %s:\n%s", lake, out)
	return ""
}

func TestGoLiveOnboardFreshMachine(t *testing.T) {
	f := newGoLiveFixture(t)
	allowed := filepath.Join(f.root, "allowed")
	lake := newOnboardLake(t, f, "a", allowed)
	onboardClient(t, f, false)
	plantAt(t, f, allowed, "fresh-1", "fresh-2")

	if lake.cmd.Process.Pid == os.Getpid() {
		t.Fatal("serve is not a subprocess")
	}
	t.Logf("serve subprocess pid %d at %s", lake.cmd.Process.Pid, lake.url)
	if err := f.register(lake, lake.mint("laptop")); err != nil {
		t.Fatal(err)
	}
	out, errs, err := f.run("sync")
	if err != nil || !strings.Contains(out, "uploaded 2") {
		t.Fatalf("first sync: %v\n%s%s", err, out, errs)
	}
	if !strings.Contains(lake.devices(), "laptop active registration profile=default machine=") {
		t.Fatalf("devices:\n%s", lake.devices())
	}
	cfg, _, _ := f.run("agent", "config")
	if !strings.Contains(cfg, "allow_source=lake:default") {
		t.Fatalf("base configuration not applied:\n%s", cfg)
	}
	out, _, err = f.run("sync")
	if err != nil || !strings.Contains(out, "manifests 0") || !strings.Contains(out, "uploaded 0") {
		t.Fatalf("second sync: %v\n%s", err, out)
	}
	t.Log("path 1: code minted on a serve subprocess, fresh client registered, device listed by name, lake profile applied, second sync posted nothing")
}

func TestGoLiveOnboardStandaloneAgentTwoLakes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("adding a lake live needs SIGHUP")
	}
	f := newGoLiveFixture(t)
	onlyA := filepath.Join(f.root, "only-a")
	both := filepath.Join(f.root, "both")
	secret := filepath.Join(both, "secret")
	lakeA := newOnboardLake(t, f, "a", onlyA, both)
	lakeB := newOnboardLake(t, f, "b", both)
	onboardClient(t, f, true, secret)
	plantAt(t, f, onlyA, "sa-1")
	plantAt(t, f, both, "sb-1")
	plantAt(t, f, secret, "ss-1")

	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: f.getenv}, "", "") }()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "\nwatching\n") })
	time.Sleep(1500 * time.Millisecond)
	if strings.Contains(buf.String(), "checked ") {
		t.Fatalf("standalone agent pushed:\n%s", buf.String())
	}

	if err := f.register(lakeA, lakeA.mint("box"), "--lake", "a"); err != nil {
		t.Fatal(err)
	}
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "reload: added a") && strings.Contains(s, "uploaded 2")
	})
	if err := f.register(lakeB, lakeB.mint("box"), "--lake", "b"); err != nil {
		t.Fatal(err)
	}
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "reload: added b") && strings.Contains(s, "lake b: checked") && strings.Contains(s, "lake b: checked 1, missing 1, uploaded 1")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if a, b := sessionsOn(t, f, "a"), sessionsOn(t, f, "b"); a != "2" || b != "1" {
		t.Fatalf("lake a has %s sessions, lake b %s; want 2 and 1\n%s", a, b, buf.String())
	}
	t.Log("path 2: standalone agent pushed nothing; lakes a and b added live by register; only-a reached a, both reached both, the locally denied session reached neither")
}

func TestGoLiveOnboardFailures(t *testing.T) {
	f := newGoLiveFixture(t)
	allowed := filepath.Join(f.root, "allowed")
	lakeA := newOnboardLake(t, f, "a", allowed)
	lakeB := newOnboardLake(t, f, "b", allowed)
	onboardClient(t, f, false)
	plantAt(t, f, allowed, "fail-1")

	code := lakeA.mint("box")
	// A tampered code.
	i := len(regcode.Prefix) + 12
	c := code[i]
	swap := byte('A')
	if c == 'A' {
		swap = 'B'
	}
	if err := f.register(lakeA, code[:i]+string(swap)+code[i+1:], "--lake", "a"); err == nil || !strings.Contains(err.Error(), "check 1") {
		t.Fatalf("tampered: %v", err)
	}
	// The wrong key at the code's URL.
	decoded, err := regcode.Decode(code)
	if err != nil {
		t.Fatal(err)
	}
	stranger, _ := identity.New(rand.Reader, time.Now())
	forged, _ := regcode.Encode(stranger, regcode.Code{URL: lakeA.url, Secret: decoded.Secret, Expires: decoded.Expires}, time.Now())
	if err := f.register(lakeA, forged, "--lake", "a"); err == nil || !strings.Contains(err.Error(), "check 3") {
		t.Fatalf("wrong key: %v", err)
	}
	// An expired code: the lake is the authority, even inside the
	// client's allowance for clock skew.
	short := lakeA.mint("short", "--expires", "1s")
	time.Sleep(1500 * time.Millisecond)
	if err := f.register(lakeA, short, "--lake", "a"); err == nil || !strings.Contains(err.Error(), "registration refused") {
		t.Fatalf("expired: %v", err)
	}
	// The good code works once.
	if err := f.register(lakeA, code, "--lake", "a"); err != nil {
		t.Fatal(err)
	}
	if err := f.register(lakeA, code, "--lake", "a2"); err == nil || !strings.Contains(err.Error(), "already this lake") {
		t.Fatalf("second use from this machine: %v", err)
	}
	other := newGoLiveFixture(t)
	onboardClient(t, other, false)
	if err := other.register(lakeA, code); err == nil || !strings.Contains(err.Error(), "registration refused") {
		t.Fatalf("used code from another machine: %v", err)
	}
	if err := f.register(lakeB, lakeB.mint("box"), "--lake", "b"); err != nil {
		t.Fatal(err)
	}
	if out, errs, err := f.run("sync"); err != nil {
		t.Fatalf("sync both: %v %s %s", err, out, errs)
	}

	// Lake b down: a keeps syncing and sync names b.
	plantAt(t, f, allowed, "fail-2")
	lakeB.stop()
	out, errs, err := f.run("sync")
	if err == nil || !strings.Contains(err.Error(), "sync failed for 1 of 2 lakes: b") || !strings.Contains(out, "lake a: checked") || !strings.Contains(out, "uploaded 1") {
		t.Fatalf("b down: %v\n%s%s", err, out, errs)
	}
	// A revoked device is refused on its next request.
	if _, errs, err := f.run("serve", "devices", "revoke", "box", "--data", lakeA.data); err != nil {
		t.Fatalf("revoke: %v %s", err, errs)
	}
	_, errs, err = f.run("sync", "--lake", "a")
	if err == nil || !strings.Contains(errs+err.Error(), "401") {
		t.Fatalf("revoked device: %v %s", err, errs)
	}
	t.Log("failures: tampered code (check 1), wrong key at the URL (check 3), expired code refused by the lake, used code refused from a second machine and a duplicate lake id refused on the first, lake b down while a synced, revoked device refused")
}

func TestGoLiveOnboardLegacyUpgrade(t *testing.T) {
	f := newGoLiveFixture(t)
	allowed := filepath.Join(f.root, "allowed")
	plantAt(t, f, allowed, "legacy-1", "legacy-2")
	// A lake with a plaintext token file, and a client configured the
	// single-lake way with the same token.
	data := filepath.Join(f.root, "lake")
	tokens := filepath.Join(f.root, "lake-tokens")
	token, err := auth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokens, []byte("# laptop\n"+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clientToken := filepath.Join(f.root, "config", "terva-lampi", "token")
	if err := auth.Write(clientToken, token); err != nil {
		t.Fatal(err)
	}
	url, cmd := goLiveServeArgs(t, data, "--token-file", tokens)
	conf := filepath.Join(f.root, "config", "terva-lampi", "config.json")
	raw, _ := os.ReadFile(conf)
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	cfg["server"] = url
	b, _ := json.Marshal(cfg)
	if err := os.WriteFile(conf, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, errs, err := f.run("sync"); err != nil || !strings.Contains(out, "uploaded 2") {
		t.Fatalf("first sync: %v %s %s", err, out, errs)
	}
	// Put the client state back in the single-lake layout of the
	// release before per-lake state, and restart the lake.
	state := filepath.Join(f.root, "state", "terva-lampi")
	legacyLayout(t, state, "default")
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	url2, _ := goLiveServeArgs(t, data, "--token-file", tokens)
	cfg["server"] = url2
	b, _ = json.Marshal(cfg)
	if err := os.WriteFile(conf, b, 0o600); err != nil {
		t.Fatal(err)
	}
	out, errs, err := f.run("sync")
	if err != nil || !strings.Contains(errs, "moved sync state") || !strings.Contains(out, "manifests 0") || !strings.Contains(out, "uploaded 0") {
		t.Fatalf("upgrade sync: %v\n%s%s", err, out, errs)
	}
	devs, _, _ := f.run("serve", "devices", "--data", data)
	if !strings.Contains(devs, "laptop active token-file profile=default machine=") || strings.Contains(devs, "machine=- ") {
		t.Fatalf("legacy token device not bound:\n%s", devs)
	}
	t.Log("upgrade: single-lake client state moved in place, legacy token-file device named and bound, the sync after the upgrade posted nothing")
}
