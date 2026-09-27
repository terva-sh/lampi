package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
)

// registerLake is a running lake with a token file and an identity.
func registerLake(t *testing.T) (dir string, lake *api.Server, url string) {
	t.Helper()
	dir = t.TempDir()
	tokens := t.TempDir()
	if err := os.WriteFile(filepath.Join(tokens, "laptop.token"), []byte(strings.Repeat("c3", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	lake, err = api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	lake.Devices = devices
	if err := lake.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.EnsureIdentity(dir); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)
	return dir, lake, srv.URL
}

func TestServeRegisterMintsACodeThatRedeemsOnce(t *testing.T) {
	dir, lake, url := registerLake(t)
	run := func(args ...string) (string, string, error) {
		var out, errb bytes.Buffer
		err := Run(append([]string{"serve"}, append(args, "--data", dir)...), Env{Stdout: &out, Stderr: &errb})
		return out.String(), errb.String(), err
	}
	if _, _, err := run("register", "--name", "newbox"); err == nil || !strings.Contains(err.Error(), "set-url") {
		t.Fatalf("mint without a URL: %v", err)
	}
	if _, _, err := run("identity", "set-url", "http://lake.example"); err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Fatalf("plain http URL: %v", err)
	}
	// A URL that answers as another lake is refused before a code exists.
	other, _, otherURL := registerLake(t)
	_ = other
	if _, _, err := run("identity", "set-url", otherURL); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run("register", "--name", "newbox"); err == nil || !strings.Contains(err.Error(), "does not reach this lake") {
		t.Fatalf("mint through another lake's URL: %v", err)
	}
	if out, _, err := run("identity", "set-url", url+"/"); err != nil || out != "public_url "+url+"\n" {
		t.Fatalf("set-url: %q %v", out, err)
	}
	if out, _, _ := run("identity"); !strings.Contains(out, "public_url "+url) {
		t.Fatalf("identity:\n%s", out)
	}
	if _, _, err := run("register", "--name", "newbox", "--profile", "ci"); err == nil || !strings.Contains(err.Error(), "no profile named ci") {
		t.Fatalf("unknown profile: %v", err)
	}
	out, stderr, err := run("register", "--name", "newbox", "--expires", "2h")
	if err != nil {
		t.Fatal(err)
	}
	code := strings.TrimSpace(out)
	if strings.Count(out, "\n") != 1 || !strings.HasPrefix(code, regcode.Prefix) {
		t.Fatalf("stdout is not one code: %q", out)
	}
	c, err := regcode.Decode(code)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != url || c.LakeID != lake.Identity.LakeID || !strings.Contains(stderr, c.Fingerprint()) {
		t.Fatalf("code %+v stderr %s", c, stderr)
	}
	if strings.Contains(stderr, c.Secret) {
		t.Fatal("stderr holds the secret")
	}
	if _, _, err := run("register", "--name", "newbox"); err == nil || !strings.Contains(err.Error(), "pending code") {
		t.Fatalf("second code for one name: %v", err)
	}

	body, _ := json.Marshal(protocol.RegisterRequest{Secret: c.Secret, TokenSHA256: auth.HashToken(strings.Repeat("e7", 32)), MachineID: "m-new"})
	for i, want := range []int{http.StatusOK, http.StatusForbidden} {
		resp, err := http.Post(c.URL+protocol.RegisterPath, "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("redeem %d: %s", i, resp.Status)
		}
	}
	list, _, _ := run("register", "--list")
	if !strings.Contains(list, " newbox used profile=default ") || !strings.Contains(list, " device=dev_") {
		t.Fatalf("list:\n%s", list)
	}
	devs, _, _ := run("devices")
	if !strings.Contains(devs, "newbox active registration profile=default machine=m-new ") {
		t.Fatalf("devices:\n%s", devs)
	}

	// Revoke a pending code; a used one cannot be.
	if _, _, err := run("register", "--name", "spare"); err != nil {
		t.Fatal(err)
	}
	out, _, err = run("register", "--revoke", "spare")
	if err != nil || !strings.HasPrefix(out, "revoked reg_") {
		t.Fatalf("revoke: %q %v", out, err)
	}
	// A second revoke by id reports that nothing changed and audits nothing.
	spare := strings.Fields(out)[1]
	if out, _, err := run("register", "--revoke", spare); err != nil || out != spare+" (spare) was already revoked\n" {
		t.Fatalf("second revoke: %q %v", out, err)
	}
	if _, _, err := run("register", "--revoke", "newbox"); err == nil {
		t.Fatal("revoked a used code")
	}
	if _, _, err := run("register", "--list", "--name", "x"); err == nil {
		t.Fatal("two modes accepted")
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	for _, kind := range []string{"registration.created", "registration.redeemed", "registration.revoked", "registration.refused"} {
		if !strings.Contains(string(raw), kind) {
			t.Fatalf("audit lacks %s:\n%s", kind, raw)
		}
	}
	if strings.Contains(string(raw), c.Secret) {
		t.Fatal("audit holds the secret")
	}
	if n := strings.Count(string(raw), "registration.revoked"); n != 1 {
		t.Fatalf("%d registration.revoked lines, want 1:\n%s", n, raw)
	}
}

func TestServeRegisterPrintsNoCodeWhoseMintIsNotAudited(t *testing.T) {
	dir, _, url := registerLake(t)
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(append([]string{"serve"}, append(args, "--data", dir)...), Env{Stdout: &out, Stderr: &bytes.Buffer{}})
		return out.String(), err
	}
	if _, err := run("identity", "set-url", url); err != nil {
		t.Fatal(err)
	}
	// An audit log that cannot be opened for append.
	if err := os.Remove(audit.Path(dir)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := run("register", "--name", "newbox")
	if err == nil || !strings.Contains(err.Error(), "revoked and not printed") {
		t.Fatalf("mint with a broken audit log: %v", err)
	}
	if out != "" {
		t.Fatalf("stdout holds a code: %q", out)
	}
	if list, _ := run("register", "--list"); !strings.Contains(list, " newbox revoked ") {
		t.Fatalf("list:\n%s", list)
	}
	// The name is free once the audit log works again.
	if err := os.Remove(audit.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if out, err := run("register", "--name", "newbox"); err != nil || !strings.HasPrefix(out, regcode.Prefix) {
		t.Fatalf("mint after repair: %q %v", out, err)
	}
}

func TestServeRegisterAuditsACodeThatExpiredUnused(t *testing.T) {
	dir, lake, _ := registerLake(t)
	now := time.Now()
	secret, _ := regcode.NewSecret()
	reg, err := lake.Catalog.CreateRegistration(t.Context(), "idle", regcode.HashSecret(secret), "", now.Add(-2*time.Hour), now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Nobody presents the code. Listing twice records its expiry once.
	for range 2 {
		var out bytes.Buffer
		if err := Run([]string{"serve", "register", "--list", "--data", dir}, Env{Stdout: &out, Stderr: &bytes.Buffer{}}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), reg.ID+" idle expired ") {
			t.Fatalf("list:\n%s", out.String())
		}
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if n := strings.Count(string(raw), `"kind":"registration.expired","device":"idle"`); n != 1 {
		t.Fatalf("%d registration.expired lines, want 1:\n%s", n, raw)
	}
	if !strings.Contains(string(raw), "registration="+reg.ID+" expires=") || strings.Contains(string(raw), secret) {
		t.Fatalf("audit:\n%s", raw)
	}
}
