package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/release"
	"terva.sh/lampi/internal/webconfig"
)

// TKT-01M3J5HXA, TKT-01M3MAPZZY: the devices page shows each device's
// agent release against the lake's, the profile it applied against the
// one the lake serves now, where its allow rules come from, and its
// last sync.
func TestDevicesPageComparesEachAgentWithTheLake(t *testing.T) {
	lake, idp, _, _ := fixture(t)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), indexes[lake], nil, &Operations{
		Release: "v0.2.0", Contacts: lake.Contacts,
	}, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	now := time.Now()
	created, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{
		{Hash: strings.Repeat("a", 64), Name: "laptop"},
		{Hash: strings.Repeat("b", 64), Name: "desktop"},
		{Hash: strings.Repeat("c", 64), Name: "quiet"},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop, desktop := created[0], created[1]
	eff, err := lake.Catalog.ResolveProfile(ctx, laptop)
	if err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.PutDeviceReport(ctx, laptop.ID, protocol.AgentReport{
		AgentVersion: "v0.1.3", ProfileVersion: eff.Version, AllowSource: "lake default", DenySource: "none",
		LastSync: &protocol.AgentSyncReport{At: now, Uploaded: 4, Refused: 221},
	}, now); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.PutDeviceReport(ctx, desktop.ID, protocol.AgentReport{
		AgentVersion: "0.0.0", ProfileVersion: "sha256:older", AllowSource: "local",
		LastError: "upload: POST /v1/hello: 503", LastErrorAt: now,
	}, now); err != nil {
		t.Fatal(err)
	}

	h := lake.Handler()
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/api/web/v1/devices", cookie)
	if w.Code != 200 {
		t.Fatalf("api: %d %s", w.Code, w.Body.String())
	}
	var got devicesView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	rows := map[string]deviceRow{}
	for _, d := range got.Devices {
		rows[d.Name] = d
	}
	if got.LakeRelease != "v0.2.0" || got.Behind != 1 || got.LocalRules != 1 {
		t.Fatalf("summary %+v", got)
	}
	if r := rows["laptop"]; r.VersionState != "behind" || r.ProfileState != "current" || r.LastSync == nil || r.LastSync.Refused != 221 {
		t.Errorf("laptop %+v", r)
	}
	if r := rows["desktop"]; r.VersionState != "unstamped" || r.ProfileState != "stale" || r.AllowSource != "local" || r.LastError == "" {
		t.Errorf("desktop %+v", r)
	}
	if r := rows["quiet"]; r.VersionState != "unknown" || r.ProfileState != "unknown" || r.Reported != "" || r.Freshness != "never" {
		t.Errorf("quiet %+v", r)
	}

	page := get(h, "/devices", cookie)
	body := page.Body.String()
	for _, want := range []string{"version-behind", "v0.1.3", "refused 221", "profile-stale", "allow rules do not apply", "error: upload: POST /v1/hello: 503", `href="/devices" aria-current="page"`} {
		if page.Code != 200 || !strings.Contains(body, want) {
			t.Fatalf("page %d missing %q", page.Code, want)
		}
	}
}

func TestVersionState(t *testing.T) {
	lake, _ := release.Parse("v0.2.0")
	for agent, want := range map[string]string{"v0.1.9": "behind", "v0.2.0": "current", "v0.2.1": "ahead", "0.0.0": "unstamped", "(devel)": "unstamped", "": "unknown"} {
		if got := versionState(agent, lake, true); got != want {
			t.Errorf("%q: %s, want %s", agent, got, want)
		}
	}
	if got := versionState("v0.1.0", release.Version{}, false); got != "unknown" {
		t.Errorf("unknown lake: %s", got)
	}
}
