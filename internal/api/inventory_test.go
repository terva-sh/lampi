package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func postInventory(t *testing.T, s *Server, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, protocol.AgentInventoryPath, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.Handler().ServeHTTP(rr, req)
	return rr
}

// TKT-01M3M7M0TH: the lake keeps each device's newest inventory, and
// holds a strict agent to its mode.
func TestAgentInventoryIsStoredForTheCallingDevice(t *testing.T) {
	s, _, _, _, _ := devicesLake(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return at }
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, RefusedSessions: 221, RefusedBytes: 9 << 20, Projects: []protocol.InventoryProject{
		{GitRemote: "github.com/acme/app", CWD: "/work/app", CWDs: 2, Harnesses: []string{"claude"}, Sessions: 12, Bytes: 4096, Newest: at, Allowed: true},
		{CWD: "/home/me/" + strings.Repeat("x", 2*maxInventoryPath), CWDs: 1, Harnesses: []string{"codex"}, Sessions: 221, Reason: "no allow rule matches"},
	}}
	body, _ := json.Marshal(inv)
	if rr := postInventory(t, s, laptopToken, body); rr.Code != http.StatusOK {
		t.Fatalf("inventory %d %s", rr.Code, rr.Body)
	}
	laptop, _ := s.Catalog.DeviceByName(ctx, "laptop")
	got, ok, err := s.Catalog.DeviceInventoryOf(ctx, laptop.ID)
	if err != nil || !ok || !got.Received.Equal(at) || len(got.Inventory.Projects) != 2 || got.Inventory.RefusedSessions != 221 {
		t.Fatalf("stored %+v %v %v", got, ok, err)
	}
	if n := len(got.Inventory.Projects[1].CWD); n > maxInventoryPath {
		t.Fatalf("cwd kept %d bytes", n)
	}

	// A strict inventory replaces it, and keeps no refused row even if
	// one was sent.
	at = at.Add(time.Minute)
	inv.Mode = protocol.InventoryStrict
	body, _ = json.Marshal(inv)
	if rr := postInventory(t, s, laptopToken, body); rr.Code != http.StatusOK {
		t.Fatalf("strict %d %s", rr.Code, rr.Body)
	}
	got, _, _ = s.Catalog.DeviceInventoryOf(ctx, laptop.ID)
	if got.Inventory.Mode != "strict" || len(got.Inventory.Projects) != 1 || !got.Inventory.Projects[0].Allowed || got.Inventory.RefusedSessions != 221 {
		t.Fatalf("strict stored %+v", got.Inventory)
	}

	// An older inventory landing late does not replace a newer one.
	if err := s.Catalog.PutDeviceInventory(ctx, laptop.ID, protocol.AgentInventory{Mode: "sociable"}, at.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ = s.Catalog.DeviceInventoryOf(ctx, laptop.ID); got.Inventory.Mode != "strict" {
		t.Fatal("an older inventory replaced a newer one")
	}
	desktop, _ := s.Catalog.DeviceByName(ctx, "desktop")
	if _, ok, _ := s.Catalog.DeviceInventoryOf(ctx, desktop.ID); ok {
		t.Fatal("filed under the wrong device")
	}
}

func TestAgentInventoryRefusals(t *testing.T) {
	s, _, _, _, _ := devicesLake(t)
	for body, code := range map[string]int{
		`{"mode":"loud","projects":[]}`: http.StatusBadRequest,
		`{"projects":[]}`:               http.StatusBadRequest,
		`{"mode":`:                      http.StatusBadRequest,
	} {
		if rr := postInventory(t, s, laptopToken, []byte(body)); rr.Code != code {
			t.Errorf("%s: %d", body, rr.Code)
		}
	}
	if rr := postInventory(t, s, "", []byte(`{"mode":"strict"}`)); rr.Code != http.StatusUnauthorized {
		t.Errorf("no token: %d", rr.Code)
	}
	big := []byte(`{"mode":"sociable","projects":[{"cwd":"` + strings.Repeat("x", int(maxInventoryBytes)) + `"}]}`)
	if rr := postInventory(t, s, laptopToken, big); rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over cap: %d", rr.Code)
	}
	// More rows than the cap are cut, and the cut is said.
	rows := make([]protocol.InventoryProject, protocol.MaxInventoryProjects+5)
	for i := range rows {
		rows[i] = protocol.InventoryProject{CWD: "/p", Allowed: true}
	}
	body, _ := json.Marshal(protocol.AgentInventory{Mode: "sociable", Projects: rows})
	if rr := postInventory(t, s, laptopToken, body); rr.Code != http.StatusOK {
		t.Fatalf("many rows: %d", rr.Code)
	}
	laptop, _ := s.Catalog.DeviceByName(t.Context(), "laptop")
	got, _, _ := s.Catalog.DeviceInventoryOf(t.Context(), laptop.ID)
	if len(got.Inventory.Projects) != protocol.MaxInventoryProjects || !got.Inventory.Truncated {
		t.Fatalf("kept %d rows, truncated %v", len(got.Inventory.Projects), got.Inventory.Truncated)
	}
}
