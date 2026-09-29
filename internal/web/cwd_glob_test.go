package web

import (
	"html"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M3NM01S: the editor reads and shows cwd_glob, refuses a pattern
// no rule may hold, and previews the folders a pattern admits.
func TestEditorSavesACWDGlob(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{CWD: "/home/me/notes/2026", CWDs: 1, Sessions: 4, Reason: config.RefusedNoMatch},
		{CWD: "/home/me/other", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch},
	}}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	if page := get(h, "/profiles/default/edit", cookie).Body.String(); !strings.Contains(page, `name="allow.0.cwd_glob"`) || !strings.Contains(page, `name="deny.0.cwd_glob"`) {
		t.Fatal("the editor has no cwd_glob fields")
	}
	w := postForm(h, "/profiles/default/preview", ruleForm(csrf, 0, []config.ProjectMatch{{CWDGlob: " /home/*/notes "}}), cookie)
	page := html.UnescapeString(w.Body.String())
	if w.Code != 200 || !strings.Contains(squash(page), "/home/me/notes/2026 folder on laptop · 4 sessions") || strings.Contains(reachSection(page), "/home/me/other") {
		t.Fatalf("preview: %d", w.Code)
	}
	if w := postForm(h, "/profiles/default/save", url.Values{"csrf": {csrf}, "base": {"0"}, "document": {hiddenValue(t, w.Body.String(), "document")}}, cookie); w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	p, err := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if err != nil || !slices.Equal(p.Config.Projects.Allow, []config.ProjectMatch{{CWDGlob: "/home/*/notes"}}) {
		t.Fatalf("saved %+v %v", p.Config.Projects.Allow, err)
	}
	if page := get(h, "/profiles/default", cookie).Body.String(); !strings.Contains(page, "/home/*/notes") {
		t.Error("the profile page does not show the pattern")
	}

	v := ruleForm(csrf, p.Revision, []config.ProjectMatch{{CWDGlob: "/*/**"}})
	if w := postForm(h, "/profiles/default/preview", v, cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "projects.allow[0].cwd_glob") {
		t.Errorf("a pattern with no plain folder: %d", w.Code)
	}
	body := `{"document":{"projects":{"deny":[{"cwd_glob":"notes/*"}]}},"base_revision":` + strconvI(p.Revision) + `}`
	if w := put(h, "/api/web/v1/profiles/default", body, cookie, map[string]string{CSRFHeader: csrf}); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_profile") {
		t.Errorf("API with a relative pattern: %d %s", w.Code, w.Body)
	}
}
