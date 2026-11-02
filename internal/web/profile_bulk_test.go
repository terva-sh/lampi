package web

import (
	"encoding/json"
	"html"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2: the Health panel groups the temporary
// rules a device still holds into one row whose button removes them all
// and hides their projects, and then offers a deny rule for their temp
// roots, which it adds only when pressed.
func TestHealthBulkFixAndTemporaryDeny(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("a", 64), Name: "mac"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	temps := []string{"/tmp/hs-exp", "/var/folders/b1/8b/T/TestLiveSmoke1298441772/001", "/tmp/terva-skill-test"}
	rules := []config.ProjectMatch{{CWDPrefix: "/srv/app"}}
	inv := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
		{CWD: "/srv/app", CWDs: 1, Sessions: 4, Allowed: true},
	}}
	for _, d := range temps {
		rules = append(rules, config.ProjectMatch{CWDPrefix: d})
		inv.Projects = append(inv.Projects, protocol.InventoryProject{CWD: d, CWDs: 1, Sessions: 1, Allowed: true})
	}
	if _, err := lake.Catalog.PutDeviceInventory(ctx, ds[0].ID, inv, now); err != nil {
		t.Fatal(err)
	}
	doc, _ := json.Marshal(config.Profile{Projects: config.Projects{Allow: rules}})
	p, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, doc, "test", "", now)
	if err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)

	edit := squash(html.UnescapeString(get(h, "/profiles/default/edit", cookie).Body.String()))
	for _, want := range []string{
		"tidy 3 findings temporary It names a temporary folder.",
		"They admit 3 projects today. Remove 3 rules and hide 3 projects and preview Show the 3 findings",
		"cwd_prefix /tmp/hs-exp temporary Admits 1 project today. Remove and hide 1 project and preview",
		"Deny cwd_prefix /tmp, cwd_prefix /var/folders and preview",
	} {
		if !strings.Contains(edit, want) {
			t.Errorf("editor lacks %q", want)
		}
	}
	if page := squash(html.UnescapeString(get(h, "/profiles/default", cookie).Body.String())); !strings.Contains(page, "3 findings temporary") || strings.Contains(page, "and preview") {
		t.Error("the profile page does not group the findings, or offers a button")
	}

	preview := func(v map[string][]string) (string, config.Profile, int) {
		t.Helper()
		w := postForm(h, "/profiles/default/preview", v, cookie)
		var got config.Profile
		if w.Code == 200 || w.Code == 409 {
			if err := json.Unmarshal([]byte(hiddenValue(t, w.Body.String(), "document")), &got); err != nil {
				t.Fatal(err)
			}
		}
		return w.Body.String(), got, w.Code
	}

	v := ruleForm(csrf, p.Revision, rules)
	v.Set("fix_all", "temporary")
	body, got, code := preview(v)
	if code != 200 || !slices.Equal(got.Projects.Allow, rules[:1]) {
		t.Fatalf("fix all: %d, allow %+v", code, got.Projects.Allow)
	}
	save := formFields(t, body, "/profiles/default/save")
	if hide := save["hide"]; len(hide) != 3 || !slices.Contains(hide, "cwd /tmp/hs-exp") {
		t.Errorf("the save hides %v", hide)
	}
	if !strings.Contains(body, "Remove 3 rules and hide 3 projects from review: temporary") {
		t.Error("the note does not say what the bulk fix did")
	}
	// With the rules gone, the queued hides still carry the offer.
	text := squash(html.UnescapeString(body))
	if !strings.Contains(text, "Deny cwd_prefix /tmp, cwd_prefix /var/folders and preview") {
		t.Error("the deny offer went with the rules")
	}

	v = ruleForm(csrf, p.Revision, rules[:1])
	v["hide"] = save["hide"]
	v.Set("deny_temporary", "1")
	body, got, code = preview(v)
	if want := []config.ProjectMatch{{CWDPrefix: "/tmp"}, {CWDPrefix: "/var/folders"}}; code != 200 || !slices.Equal(got.Projects.Deny, want) {
		t.Fatalf("deny: %d, deny %+v", code, got.Projects.Deny)
	}
	if strings.Contains(squash(html.UnescapeString(body)), "Deny cwd_prefix") {
		t.Error("the editor still offers a deny the form holds")
	}

	// Neither is applied until pressed: a plain preview changes nothing.
	if w := postForm(h, "/profiles/default/preview", ruleForm(csrf, p.Revision, rules), cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "This changes nothing") {
		t.Errorf("a plain preview: %d", w.Code)
	}

	// A bulk fix past one save's hides is refused and changes nothing.
	v = ruleForm(csrf, p.Revision, append(slices.Clone(rules), config.ProjectMatch{GitRemote: "git.example/team/extra"}))
	for i := range maxReviewBatch - 1 {
		v.Add("hide", "cwd /tmp/queued-"+strconv.Itoa(i))
	}
	v.Set("fix_all", "temporary")
	if body, got, code = preview(v); code != 409 || !strings.Contains(html.UnescapeString(body), "Fixing every temporary finding would hide 502 projects") || len(got.Projects.Allow) != 5 {
		t.Errorf("an oversized bulk fix: %d", code)
	}
}
