package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

var (
	laptopToken  = strings.Repeat("a1", 32)
	desktopToken = strings.Repeat("b2", 32)
)

// devicesLake is a lake whose token directory names two devices, laptop
// and desktop, with one blob stored that manifests can name.
func devicesLake(t *testing.T) (s *Server, dir string, logs *bytes.Buffer, sum string, size int64) {
	t.Helper()
	dir = t.TempDir()
	tokens := t.TempDir()
	for name, tok := range map[string]string{"laptop": laptopToken, "desktop": desktopToken} {
		if err := os.WriteFile(filepath.Join(tokens, name+".token"), []byte(tok+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	devices, err := auth.LoadDevices(tokens)
	if err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Devices = devices
	logs = &bytes.Buffer{}
	s.Log = slog.New(slog.NewTextHandler(logs, nil))
	if err := s.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"type\":\"meta\"}\n")
	sum, _, _ = cas.Hash(bytes.NewReader(body))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+laptopToken)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put %d %s", rr.Code, rr.Body)
	}
	return s, dir, logs, sum, int64(len(body))
}

func postAs(t *testing.T, s *Server, token, machine, sum string, size int64) *httptest.ResponseRecorder {
	t.Helper()
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       machine,
		Harness:         protocol.HarnessTerva,
		NativeSessionID: "sid-" + machine,
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/" + machine + ".jsonl", Size: size, SHA256: sum,
		}},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer "+token)
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func auditKinds(t *testing.T, dir string) []string {
	t.Helper()
	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e audit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(line, laptopToken) || strings.Contains(line, desktopToken) {
			t.Fatal("audit log holds a token")
		}
		kinds = append(kinds, e.Kind+" "+e.Device)
	}
	return kinds
}

func TestTokenFileDevicesAreNamedAndLogged(t *testing.T) {
	s, dir, logs, _, _ := devicesLake(t)
	devices, err := s.Catalog.Devices(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 || devices[0].Name != "desktop" || devices[1].Name != "laptop" || devices[1].Source != "token-file" {
		t.Fatalf("devices %+v", devices)
	}
	if !strings.Contains(logs.String(), "device=laptop device_id="+devices[1].ID) {
		t.Fatalf("access log does not name the device:\n%s", logs)
	}
	if got := strings.Join(auditKinds(t, dir), ","); got != "device.created desktop,device.created laptop" {
		t.Fatalf("audit %s", got)
	}
}

func TestADeviceBindsToItsFirstMachine(t *testing.T) {
	s, dir, _, sum, size := devicesLake(t)
	if rr := postAs(t, s, laptopToken, "machine-a", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("first manifest %d %s", rr.Code, rr.Body)
	}
	d, _ := s.Catalog.DeviceByName(t.Context(), "laptop")
	if d.MachineID != "machine-a" {
		t.Fatalf("bound to %q", d.MachineID)
	}
	// The same machine again is fine.
	if rr := postAs(t, s, laptopToken, "machine-a", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("again %d %s", rr.Code, rr.Body)
	}
	// Another machine with laptop's token: a shared legacy token.
	rr := postAs(t, s, laptopToken, "machine-b", sum, size)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "serve devices unbind laptop") {
		t.Fatalf("second machine %d %s", rr.Code, rr.Body)
	}
	// Another device claiming laptop's machine.
	rr = postAs(t, s, desktopToken, "machine-a", sum, size)
	if rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "belongs to another device") {
		t.Fatalf("taken machine %d %s", rr.Code, rr.Body)
	}
	kinds := strings.Join(auditKinds(t, dir), ",")
	if !strings.Contains(kinds, "device.bound laptop") || strings.Count(kinds, "device.refused") != 2 {
		t.Fatalf("audit %s", kinds)
	}
	// After an unbind, the next manifest binds again.
	if _, err := s.Catalog.UnbindDevice(t.Context(), "laptop"); err != nil {
		t.Fatal(err)
	}
	if rr := postAs(t, s, laptopToken, "machine-b", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("after unbind %d %s", rr.Code, rr.Body)
	}
}

func TestRevokeTakesEffectOnTheNextRequest(t *testing.T) {
	s, _, _, sum, size := devicesLake(t)
	if _, err := s.Catalog.RevokeDevice(t.Context(), "laptop", time.Now()); err != nil {
		t.Fatal(err)
	}
	rr := postAs(t, s, laptopToken, "machine-a", sum, size)
	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "revoked") {
		t.Fatalf("revoked device %d %s", rr.Code, rr.Body)
	}
	if rr := postAs(t, s, desktopToken, "machine-d", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("other device %d %s", rr.Code, rr.Body)
	}
	// A token-file reload does not bring a revoked device back.
	if err := s.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rr := postAs(t, s, laptopToken, "machine-a", sum, size); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked after reload %d", rr.Code)
	}
}

func TestATokenThatLeavesTheFileIsDetached(t *testing.T) {
	s, dir, _, _, _ := devicesLake(t)
	next := &auth.Devices{}
	next.Allow(desktopToken)
	s.Devices.Replace(next)
	if err := s.SyncDevices(t.Context()); err != nil {
		t.Fatal(err)
	}
	d, _ := s.Catalog.DeviceByName(t.Context(), "laptop")
	if d.State() != "detached" {
		t.Fatalf("laptop %s", d.State())
	}
	if !strings.Contains(strings.Join(auditKinds(t, dir), ","), "device.detached laptop") {
		t.Fatal("no detach event")
	}
}

func TestATokenWithNoDeviceRecordIsRefused(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tok := strings.Repeat("d4", 32)
	// Publish a token without recording it, which serve never does.
	s.Devices = &auth.Devices{}
	s.Devices.Allow(tok)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized || !strings.Contains(rr.Body.String(), "no device record") {
		t.Fatalf("unrecorded token %d %s", rr.Code, rr.Body)
	}
	// Allow records it.
	s.Allow(tok)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("after Allow %d %s", rr.Code, rr.Body)
	}
}

func TestBindingIsReadFreshForEachManifest(t *testing.T) {
	s, _, _, sum, size := devicesLake(t)
	if rr := postAs(t, s, laptopToken, "machine-a", sum, size); rr.Code != http.StatusOK {
		t.Fatalf("bind %d %s", rr.Code, rr.Body)
	}
	// An operator unbinds laptop and it binds to machine-b elsewhere.
	d, _ := s.Catalog.UnbindDevice(t.Context(), "laptop")
	if _, err := s.Catalog.BindMachine(t.Context(), d.ID, "machine-b"); err != nil {
		t.Fatal(err)
	}
	if rr := postAs(t, s, laptopToken, "machine-a", sum, size); rr.Code != http.StatusForbidden {
		t.Fatalf("stale machine %d %s", rr.Code, rr.Body)
	}
}
