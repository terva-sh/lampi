package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/webconfig"
)

// TKT-01M3M7M0ZY: viewers see every profile, its rules, the devices it
// reaches and its revisions, and a banner while a profiles file sits
// unread.
func TestProfilesPagesShowRulesDevicesAndRevisions(t *testing.T) {
	lake, idp, _, _ := fixture(t)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	ignored := []IgnoredProfiles{{Path: "/var/lib/lake/profiles.json", Import: "terva-lampi serve profiles import /var/lib/lake/profiles.json --data /var/lib/lake"}}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), indexes[lake], nil, &Operations{
		IgnoredProfiles: func() []IgnoredProfiles { return ignored },
	}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	now := time.Now()
	doc := `{"harnesses":{"codex":{"enabled":false}},"agent":{"debounce":"5s"},"projects":{"allow":[{"cwd_prefix":"/work","git_remote_prefix":"github.com/acme"}],"deny":[{"cwd_hash":"0011223344556677"}]}}`
	if _, _, err := lake.Catalog.PutProfile(ctx, "ci", []byte(`{}`), "web:first", "first", now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := lake.Catalog.PutProfile(ctx, "ci", []byte(doc), "web:second", "tighten", now); err != nil {
		t.Fatal(err)
	}
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "runner"},
		{Hash: strings.Repeat("b", 64), Name: "laptop"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.SetDeviceProfile(ctx, "runner", "ci", "ci", "test", now); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.PutDeviceReport(ctx, created[0].ID, protocol.AgentReport{AllowSource: "local"}, now); err != nil {
		t.Fatal(err)
	}

	h := lake.Handler()
	cookie, _ := signIn(t, idp, h)
	var list profilesView
	if err := json.Unmarshal(get(h, "/api/web/v1/profiles", cookie).Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Profiles) != 2 || list.Profiles[0].Name != "default" || list.Profiles[0].Stored || list.Profiles[0].Devices < 1 ||
		list.Profiles[1].Name != "ci" || list.Profiles[1].Devices != 1 || list.Profiles[1].UpdatedBy != "web:second" || len(list.Ignored) != 1 {
		t.Fatalf("list %+v", list)
	}

	w := get(h, "/api/web/v1/profiles/ci", cookie)
	var one profileView
	if err := json.Unmarshal(w.Body.Bytes(), &one); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(one.Revisions) != 2 || one.Revisions[0].Note != "tighten" || len(one.DeviceList) != 1 || one.DeviceList[0].AllowSource != "local" {
		t.Fatalf("profile %+v", one)
	}
	for _, hn := range one.Harnesses {
		if hn.ID == "codex" && (!hn.Set || hn.Enabled) || hn.ID == "claude" && hn.Set {
			t.Errorf("harness %+v", hn)
		}
	}
	for path, code := range map[string]int{"/api/web/v1/profiles/nope": 404, "/api/web/v1/profiles/Bad!": 404, "/api/web/v1/profiles/ci?x=1": 400, "/profiles/nope": 404} {
		if w := get(h, path, cookie); w.Code != code {
			t.Errorf("%s: %d, want %d", path, w.Code, code)
		}
	}

	page := get(h, "/profiles/ci", cookie).Body.String()
	for _, want := range []string{"/work", "github.com/acme", "0011223344556677", "5s", "allow rules do not reach it", "tighten", "web:second", "A profiles file is not in force", "serve profiles import /var/lib/lake/profiles.json", "Device overrides"} {
		if !strings.Contains(page, want) {
			t.Errorf("profile page missing %q", want)
		}
	}
	listPage := get(h, "/profiles", cookie).Body.String()
	for _, want := range []string{`href="/profiles/ci"`, "Not saved yet", `href="/profiles" aria-current="page"`} {
		if !strings.Contains(listPage, want) {
			t.Errorf("list page missing %q", want)
		}
	}
	// A default no one saved still has a page.
	if w := get(h, "/profiles/default", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "Never saved") {
		t.Errorf("default: %d", w.Code)
	}
}
