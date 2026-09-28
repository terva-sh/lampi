package web

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
)

// TKT-01M3J5HXA: an operator revokes, unbinds and sets the profile of a
// device from the dashboard. Each change is CSRF-checked and audited
// with the signed-in operator as its actor.
func TestOperatorChangesDevices(t *testing.T) {
	lake, idp, h, dir := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"},
		{Hash: strings.Repeat("b", 64), Name: "desktop"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	laptop, desktop := created[0], created[1]
	if _, err := lake.Catalog.BindMachine(ctx, laptop.ID, "01MACHINE", time.Now()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	hdr := map[string]string{CSRFHeader: csrf}
	api := "/api/web/v1/devices/"

	if w := post(h, api+laptop.ID+"/unbind", "", cookie, nil); w.Code != 403 || !strings.Contains(w.Body.String(), "csrf_failed") {
		t.Fatalf("no csrf: %d %s", w.Code, w.Body)
	}
	for path, want := range map[string]string{
		api + "dev_nope/revoke":       "not_found",
		api + laptop.Name + "/revoke": "not_found",
		api + laptop.ID + "/rename":   "not_found",
	} {
		if w := post(h, path, "", cookie, hdr); w.Code != 404 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	for body, want := range map[string]string{
		`{"profile":"nope"}`:         "unknown_profile",
		`{}`:                         "invalid_request",
		`{"profile":"ci","extra":1}`: "invalid_request",
		`{"profile":"ci"}{}`:         "invalid_request",
	} {
		if w := post(h, api+laptop.ID+"/profile", body, cookie, hdr); w.Code != 400 || !strings.Contains(w.Body.String(), want) {
			t.Errorf("profile %s: %d %s", body, w.Code, w.Body)
		}
	}
	if w := post(h, api+laptop.ID+"/unbind", `{"profile":"ci"}`, cookie, hdr); w.Code != 400 {
		t.Errorf("unbind with a profile: %d %s", w.Code, w.Body)
	}

	if w := post(h, api+laptop.ID+"/profile", `{"profile":"ci"}`, cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"profile":"ci"`) {
		t.Fatalf("set profile: %d %s", w.Code, w.Body)
	}
	if w := post(h, api+laptop.ID+"/unbind", "", cookie, hdr); w.Code != 200 || strings.Contains(w.Body.String(), "01MACHINE") {
		t.Fatalf("unbind: %d %s", w.Code, w.Body)
	}
	if w := post(h, api+laptop.ID+"/unbind", "", cookie, hdr); w.Code != 409 || !strings.Contains(w.Body.String(), "not_bound") {
		t.Fatalf("unbind twice: %d %s", w.Code, w.Body)
	}
	if w := post(h, api+desktop.ID+"/revoke", "", cookie, hdr); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"revoked"`) {
		t.Fatalf("revoke: %d %s", w.Code, w.Body)
	}
	if w := post(h, api+desktop.ID+"/profile", `{"profile":"ci"}`, cookie, hdr); w.Code != 409 || !strings.Contains(w.Body.String(), `"revoked"`) {
		t.Fatalf("change a revoked device: %d %s", w.Code, w.Body)
	}

	got, err := lake.Catalog.DeviceByName(ctx, "laptop")
	if err != nil || got.Profile != "ci" || got.MachineID != "" {
		t.Fatalf("laptop %+v %v", got, err)
	}
	// Back to the default, which the catalog stores as empty.
	if w := post(h, api+laptop.ID+"/profile", `{"profile":"default"}`, cookie, hdr); w.Code != 200 {
		t.Fatalf("default: %d %s", w.Code, w.Body)
	}
	if got, _ := lake.Catalog.DeviceByName(ctx, "laptop"); got.Profile != "" {
		t.Fatalf("default stored as %q", got.Profile)
	}

	log, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	changes := 0
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		var e audit.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		switch e.Kind {
		case audit.DeviceProfile, audit.DeviceUnbound, audit.DeviceRevoked:
			changes++
			if !strings.HasPrefix(e.Actor, "web:") {
				t.Errorf("%s by %q, not the operator", e.Kind, e.Actor)
			}
		}
	}
	if changes != 4 {
		t.Errorf("%d changes audited, want 4:\n%s", changes, log)
	}
}

func TestDevicesPageFormsForOperatorsOnly(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	created, err := lake.Catalog.SyncTokenFile(t.Context(), []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	id := created[0].ID
	cookie, _ := signIn(t, idp, h)
	page := get(h, "/devices", cookie).Body.String()
	for _, want := range []string{`action="/devices/` + id + `/profile"`, `action="/devices/` + id + `/revoke"`, `<option value="ci"`, `id="` + id + `"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("operator page missing %q", want)
		}
	}
	if strings.Contains(page, "/unbind") {
		t.Fatal("unbind offered for a device bound to no machine")
	}
	csrf := csrfOf(t, h, cookie)
	if w := postForm(h, "/devices/"+id+"/profile", url.Values{"csrf": {"wrong"}, "profile": {"ci"}}, cookie); w.Code != 403 {
		t.Fatalf("wrong csrf: %d", w.Code)
	}
	w := postForm(h, "/devices/"+id+"/profile", url.Values{"csrf": {csrf}, "profile": {"ci"}}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/devices#"+id {
		t.Fatalf("set: %d %s", w.Code, w.Header().Get("Location"))
	}
	w = postForm(h, "/devices/"+id+"/profile", url.Values{"csrf": {csrf}, "profile": {"gone"}}, cookie)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "not in the lake any more") {
		t.Fatalf("unknown profile: %d", w.Code)
	}

	_, vidp, vh, _ := operatorLake(t, "", "readers")
	viewer, _ := signIn(t, vidp, vh)
	if body := get(vh, "/devices", viewer).Body.String(); strings.Contains(body, `/revoke"`) || strings.Contains(body, "<th scope=\"col\">Actions") {
		t.Fatal("viewer sees operator forms")
	}
	vcsrf := csrfOf(t, vh, viewer)
	for _, w := range []*httptest.ResponseRecorder{
		post(vh, "/api/web/v1/devices/dev_x/revoke", "", viewer, map[string]string{CSRFHeader: vcsrf}),
		postForm(vh, "/devices/dev_x/revoke", url.Values{"csrf": {vcsrf}}, viewer),
	} {
		if w.Code != 404 {
			t.Fatalf("viewer: %d", w.Code)
		}
	}
}
