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
	if !strings.HasPrefix(out.String(), "laptop active token-file machine=- id=dev_") {
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
