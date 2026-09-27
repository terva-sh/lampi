package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
)

func TestServeDevicesRevokeStopsARunningLake(t *testing.T) {
	dir := t.TempDir()
	tokens := t.TempDir()
	laptop := strings.Repeat("c3", 32)
	if err := os.WriteFile(filepath.Join(tokens, "laptop.token"), []byte(laptop+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lake.Close()
	lake.Devices = devices
	if err := lake.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	stats := func() int {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer "+laptop)
		lake.Handler().ServeHTTP(rr, req)
		return rr.Code
	}
	if stats() != http.StatusOK {
		t.Fatal("laptop refused before revoke")
	}

	var out bytes.Buffer
	if err := Run([]string{"serve", "devices", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "laptop active token-file profile=default machine=- id=dev_") {
		t.Fatalf("list:\n%s", out.String())
	}
	out.Reset()
	if err := Run([]string{"serve", "devices", "revoke", "laptop", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "revoked laptop\n" {
		t.Fatalf("revoke output %q", out.String())
	}
	if stats() != http.StatusUnauthorized {
		t.Fatal("revoked device still accepted by the running lake")
	}
	out.Reset()
	if err := Run([]string{"serve", "devices", "list", "--data", dir}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "laptop revoked ") {
		t.Fatalf("list after revoke:\n%s", out.String())
	}
	if err := Run([]string{"serve", "devices", "unbind", "nope", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil || !strings.Contains(err.Error(), "no device named nope") {
		t.Fatalf("unbind unknown: %v", err)
	}
	if err := Run([]string{"serve", "devices", "revoke", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()}); err == nil {
		t.Fatal("revoke without a name accepted")
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if !strings.Contains(string(raw), `"kind":"device.revoked","device":"laptop"`) || !strings.Contains(string(raw), `"actor":"serve devices revoke"`) {
		t.Fatalf("audit:\n%s", raw)
	}

	backup := filepath.Join(t.TempDir(), "b")
	out.Reset()
	if err := Run([]string{"serve", "backup", "--data", dir, "--out", backup}, Env{Stdout: &out, Stderr: ioDiscard()}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(audit.Path(backup)); err != nil || !strings.Contains(out.String(), "audit: ") {
		t.Fatalf("audit not backed up: %v\n%s", err, out.String())
	}
}

func TestServeDevicesAuditFailureAdvice(t *testing.T) {
	dir := t.TempDir()
	tokens := t.TempDir()
	for _, n := range []string{"laptop", "desk"} {
		if err := os.WriteFile(filepath.Join(tokens, n+".token"), []byte(strings.Repeat(map[string]string{"laptop": "c3", "desk": "d4"}[n], 32)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Devices = devices
	if err := lake.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	lake.Close()
	// audit.jsonl as a directory: every append fails.
	if err := os.Remove(audit.Path(dir)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	err = Run([]string{"serve", "devices", "unbind", "laptop", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "Do not run unbind again") || strings.Contains(err.Error(), "only retries the record") {
		t.Fatalf("unbind: %v", err)
	}
	err = Run([]string{"serve", "devices", "revoke", "desk", "--data", dir}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "running revoke again only retries the record") {
		t.Fatalf("revoke: %v", err)
	}
}

func TestServeDevicesSetProfile(t *testing.T) {
	dir := t.TempDir()
	tokens := t.TempDir()
	if err := os.WriteFile(filepath.Join(tokens, "laptop.token"), []byte(strings.Repeat("c3", 32)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Devices = devices
	if err := lake.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	lake.Close()
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := Run(append([]string{"serve", "devices"}, append(args, "--data", dir)...), Env{Stdout: &out, Stderr: ioDiscard()})
		return out.String(), err
	}
	if _, err := run("set-profile", "laptop", "ci"); err == nil || !strings.Contains(err.Error(), "no profile named ci") {
		t.Fatalf("unknown profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "profiles.json"), []byte(`{"profiles":{"ci":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run("set-profile", "laptop", "ci"); err != nil || out != "set laptop to profile ci\n" {
		t.Fatalf("set-profile: %q %v", out, err)
	}
	if out, _ := run("list"); !strings.Contains(out, "laptop active token-file profile=ci ") {
		t.Fatalf("list:\n%s", out)
	}
	if _, err := run("set-profile", "nope", "ci"); err == nil || !strings.Contains(err.Error(), "no device named nope") {
		t.Fatalf("unknown device: %v", err)
	}
	if _, err := run("set-profile", "laptop"); err == nil {
		t.Fatal("set-profile without a profile accepted")
	}
	if out, _ := run("set-profile", "laptop", "default"); out != "set laptop to profile default\n" {
		t.Fatalf("back to default: %q", out)
	}
	raw, _ := os.ReadFile(audit.Path(dir))
	if !strings.Contains(string(raw), `"kind":"device.profile","device":"laptop"`) || !strings.Contains(string(raw), `"detail":"profile=ci"`) {
		t.Fatalf("audit:\n%s", raw)
	}
}

func TestServeRefusesAProfilesFileWithAFieldOutsideTheAllowedSet(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(bad, []byte(`{"profiles":{"default":{"server":"https://elsewhere.example"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Run([]string{"serve", "--data", dir, "--addr", "127.0.0.1:0", "--profiles", bad}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), `unknown field "server"`) {
		t.Fatalf("serve with a bad profiles file: %v", err)
	}
}
