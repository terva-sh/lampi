package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M3M7M11S: a device's page shows its status, its configuration
// and the projects its agent reported, the refused ones only when the
// agent is sociable.
func TestDevicePageShowsItsInventory(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"},
		{Hash: strings.Repeat("b", 64), Name: "vault"},
		{Hash: strings.Repeat("c", 64), Name: "quiet"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop, vault, quiet := created[0], created[1], created[2]
	if err := lake.Catalog.PutDeviceReport(ctx, laptop.ID, protocol.AgentReport{
		AgentVersion: "v0.1.3", AllowSource: "lake default", DenySource: "none", Inventory: "sociable",
		LastSync: &protocol.AgentSyncReport{At: now, Uploaded: 4, Refused: 221},
	}, now); err != nil {
		t.Fatal(err)
	}
	rows := []protocol.InventoryProject{
		{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 2, Harnesses: []string{"claude"}, Sessions: 12, Bytes: 4096, Newest: now, Allowed: true},
		{CWD: "/home/me/scratch", CWDs: 1, Harnesses: []string{"codex"}, Sessions: 221, Bytes: 9 << 20, Reason: "no allow rule matches"},
	}
	for _, d := range []struct {
		id, mode string
	}{{laptop.ID, protocol.InventorySociable}, {vault.ID, protocol.InventoryStrict}} {
		inv := protocol.AgentInventory{Mode: d.mode, GeneratedAt: now, Projects: rows, RefusedSessions: 221, RefusedBytes: 9 << 20}
		if d.mode == protocol.InventoryStrict {
			inv.Projects = rows[:1]
		}
		if _, err := lake.Catalog.PutDeviceInventory(ctx, d.id, inv, now); err != nil {
			t.Fatal(err)
		}
	}
	cookie, _ := signIn(t, idp, h)

	w := get(h, "/api/web/v1/devices/"+laptop.ID, cookie)
	if w.Code != 200 {
		t.Fatalf("api %d %s", w.Code, w.Body)
	}
	var v deviceView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Device.Name != "laptop" || v.Device.LastSync == nil || v.Device.LastSync.Refused != 221 || v.Device.AllowSource != "lake default" {
		t.Fatalf("device %+v", v.Device)
	}
	if i := v.Inventory; i == nil || i.Mode != "sociable" || len(i.Projects) != 2 || i.Allowed != 1 || i.Refused != 1 || i.RefusedSessions != 221 {
		t.Fatalf("inventory %+v", v.Inventory)
	}
	if w := get(h, "/api/web/v1/devices/"+quiet.ID, cookie); w.Code != 200 || !strings.Contains(w.Body.String(), `"inventory":null`) {
		t.Fatalf("no inventory: %d %s", w.Code, w.Body)
	}
	for path, code := range map[string]int{
		"/api/web/v1/devices/dev_nope":               404,
		"/api/web/v1/devices/" + laptop.Name:         404,
		"/api/web/v1/devices/" + laptop.ID + "?x=1":  400,
		"/devices/dev_nope":                          404,
		"/devices/" + laptop.ID + "?show=all":        400,
		"/devices/" + laptop.ID + "?show=refused&x=": 400,
	} {
		if w := get(h, path, cookie); w.Code != code {
			t.Errorf("%s: %d, want %d", path, w.Code, code)
		}
	}

	if !strings.Contains(get(h, "/devices/"+quiet.ID, cookie).Body.String(), "No inventory yet") {
		t.Error("device with no inventory")
	}
	page := get(h, "/devices/"+laptop.ID, cookie).Body.String()
	for _, want := range []string{"git.example/team/app", "/home/me/scratch", "no allow rule matches", "2 checkouts", "lake default", "Refused only", `action="/devices/` + laptop.ID + `/profile"`, `name="from" value="device"`} {
		if !strings.Contains(page, want) {
			t.Errorf("device page missing %q", want)
		}
	}
	refused := get(h, "/devices/"+laptop.ID+"?show=refused", cookie).Body.String()
	if !strings.Contains(refused, "/home/me/scratch") || strings.Contains(refused, "git.example/team/app") {
		t.Error("refused only still lists the allowed project, or lost the refused one")
	}
	// A machine with nothing refused says so, rather than showing an
	// empty table.
	if _, err := lake.Catalog.PutDeviceInventory(ctx, quiet.ID, protocol.AgentInventory{Mode: "sociable", GeneratedAt: now, Projects: rows[:1]}, now); err != nil {
		t.Fatal(err)
	}
	if body := get(h, "/devices/"+quiet.ID+"?show=refused", cookie).Body.String(); !strings.Contains(body, "No project on this machine is refused") || strings.Contains(body, "inventory-table") {
		t.Error("refused only with nothing refused")
	}
	strict := get(h, "/devices/"+vault.ID, cookie).Body.String()
	if body := get(h, "/devices/"+vault.ID+"?show=refused", cookie).Body.String(); strings.Contains(body, "No project on this machine is refused") || !strings.Contains(body, "git.example/team/app") {
		t.Error("a strict device's refused view claims nothing is refused")
	}
	if !strings.Contains(strict, "git.example/team/app") || strings.Contains(strict, "/home/me/scratch") ||
		!strings.Contains(strict, "This device is strict") || strings.Contains(strict, "Refused only") {
		t.Error("strict device page")
	}

	// The devices list and the operations machines link here.
	for _, path := range []string{"/devices", "/operations"} {
		if body := get(h, path, cookie).Body.String(); !strings.Contains(body, `href="/devices/`+laptop.ID+`"`) {
			t.Errorf("%s does not link to the device page", path)
		}
	}
}

// An action from a device's page comes back to it, and a refusal is
// shown there.
func TestDevicePageActionsReturnToIt(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	created, err := lake.Catalog.SyncTokenFile(t.Context(), []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := created[0].ID
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	w := postForm(h, "/devices/"+id+"/profile", url.Values{"csrf": {csrf}, "profile": {"ci"}, "from": {"device"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/devices/"+id {
		t.Fatalf("set: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = postForm(h, "/devices/"+id+"/profile", url.Values{"csrf": {csrf}, "profile": {"gone"}, "from": {"device"}}, cookie)
	if body := w.Body.String(); w.Code != 400 || !strings.Contains(body, "not in the lake any more") || !strings.Contains(body, "Projects on this machine") {
		t.Fatalf("refused on the device page: %d", w.Code)
	}

	// A viewer reads the page without the forms.
	vlake, vidp, vh, _ := operatorLake(t, "", "readers")
	vcreated, err := vlake.Catalog.SyncTokenFile(t.Context(), []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	viewer, _ := signIn(t, vidp, vh)
	w = get(vh, "/devices/"+vcreated[0].ID, viewer)
	if w.Code != 200 || strings.Contains(w.Body.String(), `/revoke"`) || strings.Contains(w.Body.String(), `name="from"`) {
		t.Fatalf("viewer: %d", w.Code)
	}
}
