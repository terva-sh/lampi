package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
)

// serveIdentity runs serve identity SUB on the fixture's lake and then
// loads the identity again, as a restart of serve would.
func (f *regFixture) serveIdentity(args ...string) string {
	f.t.Helper()
	var out bytes.Buffer
	if err := Run(append([]string{"serve", "identity"}, append(args, "--data", f.lakeDir)...), Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		f.t.Fatalf("serve identity %v: %v", args, err)
	}
	id, err := identity.Load(f.lakeDir)
	if err != nil {
		f.t.Fatal(err)
	}
	f.lake.SetIdentity(id)
	return out.String()
}

func (f *regFixture) pinnedKey() string {
	f.t.Helper()
	file, err := config.LoadFile(agentGetenv(f.home, f.cfg, f.state))
	if err != nil {
		f.t.Fatal(err)
	}
	return file.Lakes["default"].KeyID
}

func (f *regFixture) sync() (string, error) {
	f.stdout.Reset()
	f.stderr.Reset()
	err := Run([]string{"sync"}, f.env(""))
	return f.stdout.String() + f.stderr.String(), err
}

func TestRotationMovesThePinAndCompromiseStopsIt(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatal(err)
	}
	first := f.pinnedKey()
	if out, err := f.sync(); err != nil || !strings.Contains(out, "uploaded 1") {
		t.Fatalf("first sync: %v\n%s", err, out)
	}

	// Rotate: the new key is endorsed by the pinned one, and the agent
	// moves its pin without registering again.
	out := f.serveIdentity("rotate", "--overlap", "1h")
	if !strings.Contains(out, "endorsed_by "+first) {
		t.Fatalf("rotate:\n%s", out)
	}
	if out, err := f.sync(); err != nil || !strings.Contains(out, "pin moved to key") {
		t.Fatalf("sync after rotate: %v\n%s", err, out)
	}
	second := f.pinnedKey()
	if second == first {
		t.Fatal("pin did not move")
	}
	// The old key retired: hello no longer carries its signature, and
	// the moved pin does not need it.
	f.serveIdentity("retire", first)
	if out, err := f.sync(); err != nil {
		t.Fatalf("sync after retire: %v\n%s", err, out)
	}

	// Compromise of the current key: rotate away first, then retire it
	// as compromised. This machine is pinned to it and has not moved, so
	// it stops and says to register again.
	f.serveIdentity("rotate", "--overlap", "1h")
	f.serveIdentity("retire", second, "--compromised")
	out, err := f.sync()
	if err == nil || !strings.Contains(out, "compromised") || !strings.Contains(out, "register --replace") {
		t.Fatalf("sync on a compromised pin: %v\n%s", err, out)
	}
	if f.pinnedKey() != second {
		t.Fatal("a compromised pin moved")
	}
	raw, _ := os.ReadFile(audit.Path(f.lakeDir))
	if strings.Count(string(raw), `"kind":"key.added"`) != 2 || !strings.Contains(string(raw), "compromised") {
		t.Fatalf("audit:\n%s", raw)
	}
}

func TestCodesFromARetiredKeyAreRefused(t *testing.T) {
	f := newRegFixture(t)
	code := f.mint("box")
	old := f.lake.Identity().Keys[0].ID
	f.serveIdentity("rotate", "--overlap", "1h")
	f.serveIdentity("retire", old)
	// register refuses: the key list shows the code's key retired.
	if err := f.register(code+"\n", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "check 3") || !strings.Contains(err.Error(), "retired") {
		t.Fatalf("register: %v", err)
	}
	// The lake refuses too, for a client that skips the check.
	c, err := regcode.Decode(code)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(protocol.RegisterRequest{Secret: c.Secret, TokenSHA256: auth.HashToken(strings.Repeat("e7", 32)), MachineID: "m"})
	resp, err := http.Post(f.url+protocol.RegisterPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("lake: %s", resp.Status)
	}
	raw, _ := os.ReadFile(audit.Path(f.lakeDir))
	if !strings.Contains(string(raw), "reason=signing key retired") {
		t.Fatalf("audit:\n%s", raw)
	}
	// A code from the new key works.
	if err := f.register(f.mint("box2")+"\n", "--fingerprint", identity.Fingerprint(f.lake.Identity().Keys[1].Pub)); err != nil {
		t.Fatal(err)
	}
}

func TestARunningAgentFollowsARotation(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatal(err)
	}
	first := f.pinnedKey()
	old := profileEvery
	profileEvery = 100 * time.Millisecond
	defer func() { profileEvery = old }()
	var buf memBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runAgentLoop(ctx, Env{Stdout: &buf, Stderr: &buf, Getenv: agentGetenv(f.home, f.cfg, f.state)}, "", "")
	}()
	waitOut(t, &buf, func(s string) bool { return strings.Contains(s, "uploaded 1") })
	f.serveIdentity("rotate", "--overlap", "1h")
	waitOut(t, &buf, func(s string) bool {
		return strings.Contains(s, "key list: pin moved") && strings.Contains(s, "reload: restarted default")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if f.pinnedKey() == first {
		t.Fatal("config.json still pins the old key")
	}
}

func TestServeReloadsIdentityOnHangup(t *testing.T) {
	dir, lake, _ := registerLake(t)
	if err := Run([]string{"serve", "identity", "rotate", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	var errb bytes.Buffer
	reloadIdentity(Env{Stderr: &errb}, dir, lake)
	if len(lake.Identity().Keys) != 2 || !strings.Contains(errb.String(), "reloaded identity, 2 active keys") {
		t.Fatalf("keys %d: %s", len(lake.Identity().Keys), errb.String())
	}
	// identity.json from another lake is refused and the keys stay.
	otherDir, _, _ := registerLake(t)
	errb.Reset()
	reloadIdentity(Env{Stderr: &errb}, otherDir, lake)
	if len(lake.Identity().Keys) != 2 || !strings.Contains(errb.String(), "reload refused") {
		t.Fatalf("other lake: %s", errb.String())
	}
}

func TestThePinStaysWhenTheProfileUnderTheNewKeyCannotBeFetched(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatal(err)
	}
	first := f.pinnedKey()
	if out, err := f.sync(); err != nil || !strings.Contains(out, "uploaded 1") {
		t.Fatalf("first sync: %v\n%s", err, out)
	}
	f.serveIdentity("rotate", "--overlap", "1h")
	// The device's profile has left the lake's profiles file, so the
	// profile under the new key cannot be fetched: the pin stays, and
	// the sync goes on under the old key while the overlap lasts.
	profiles := f.lake.Profiles()
	f.lake.SetProfiles(config.Profiles{"other": {}})
	out, err := f.sync()
	if err != nil || !strings.Contains(out, "the pin stays on key "+first) || strings.Contains(out, "pin moved") {
		t.Fatalf("sync without a profile: %v\n%s", err, out)
	}
	if f.pinnedKey() != first {
		t.Fatal("the pin moved without the profile under the new key")
	}
	// With the profile back, the next sync moves it.
	f.lake.SetProfiles(profiles)
	if out, err := f.sync(); err != nil || !strings.Contains(out, "pin moved to key") {
		t.Fatalf("sync with the profile back: %v\n%s", err, out)
	}
	if f.pinnedKey() == first {
		t.Fatal("pin did not move")
	}
}

func TestTheCachedProfileStaysWhenTheMovedPinCannotBeWritten(t *testing.T) {
	f := newRegFixture(t)
	if err := f.register(f.mint("box")+"\n", "--fingerprint", f.fingerprint()); err != nil {
		t.Fatal(err)
	}
	if out, err := f.sync(); err != nil || !strings.Contains(out, "uploaded 1") {
		t.Fatalf("first sync: %v\n%s", err, out)
	}
	getenv := agentGetenv(f.home, f.cfg, f.state)
	file, err := config.LoadFile(getenv)
	if err != nil {
		t.Fatal(err)
	}
	lakes, err := config.ResolveLakes(file, getenv, config.LakeFlags{})
	if err != nil {
		t.Fatal(err)
	}
	lake := lakes[0]
	state, err := config.StateDir(getenv)
	if err != nil {
		t.Fatal(err)
	}
	dir := lakestate.Dir(state, lake.Name)
	if _, ok, err := lakeprofile.Load(dir, lake); err != nil || !ok {
		t.Fatalf("no cached profile after the first sync: %v", err)
	}
	// Rotate and retire the pinned key, so a profile fetched now carries
	// only the new key's signature.
	f.serveIdentity("rotate", "--overlap", "1h")
	f.serveIdentity("retire", lake.KeyID)
	// The profile under the new key is fetched, and then config.json
	// cannot be read, so the pin stays on the old key and the cache
	// must still verify under it.
	if err := os.WriteFile(filepath.Join(f.cfg, "terva-lampi", "config.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, moved, err := refreshPin(context.Background(), f.env(""), lake); err == nil || moved {
		t.Fatalf("refreshPin with an unreadable config: moved %v, err %v", moved, err)
	}
	if _, ok, err := lakeprofile.Load(dir, lake); err != nil || !ok {
		t.Fatalf("the cached profile no longer verifies under the pin: ok %v, err %v", ok, err)
	}
}

func TestCodesFromAKeyPastItsOverlapAreRefused(t *testing.T) {
	f := newRegFixture(t)
	code := f.mint("box")
	// No overlap: the code's key is still listed active, but its window
	// has ended. The lake signs the key list only with keys in their
	// window by its own clock, the rule it redeems codes by, so register
	// refuses at the signature.
	f.serveIdentity("rotate", "--overlap", "0s")
	if err := f.register(code+"\n", "--fingerprint", f.fingerprint()); err == nil || !strings.Contains(err.Error(), "check 3") || !strings.Contains(err.Error(), "not signed by key") {
		t.Fatalf("register: %v", err)
	}
}
