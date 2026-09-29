package web

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M3NM01K: the preview lists the projects a change to the rules
// admits and drops, from the newest inventory of each device on the
// profile, and names the devices it cannot speak for.
func TestPreviewListsTheProjectsAChangeAdmitsAndDrops(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "readers", "admins")
	ctx := t.Context()
	now := time.Now()
	var hashes []catalog.TokenEntry
	for i, name := range []string{"laptop", "desk", "ci", "fresh"} {
		hashes = append(hashes, catalog.TokenEntry{Hash: strings.Repeat(string(rune('a'+i)), 64), Name: name})
	}
	devices, err := lake.Catalog.SyncTokenFile(ctx, hashes, now)
	if err != nil {
		t.Fatal(err)
	}
	laptop, desk, ci := devices[0], devices[1], devices[2]
	stored := `{"projects":{"allow":[{"git_remote":"git.example/team/lib"},{"cwd_prefix":"/home/me/notes"}]}}`
	// The profile is saved, then the devices report applying it, then
	// they send the inventories the preview reads.
	saved, reported := now.Add(-2*time.Minute), now.Add(-time.Minute)
	first, _, err := lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(stored), "test", "", saved)
	if err != nil {
		t.Fatal(err)
	}
	lakeRules := config.OriginLake("default")
	report := func(id string, r protocol.AgentReport) {
		t.Helper()
		if err := lake.Catalog.PutDeviceReport(ctx, id, r, reported); err != nil {
			t.Fatal(err)
		}
	}
	// laptop applied the stored profile and has deny rules of its own.
	report(laptop.ID, protocol.AgentReport{AllowSource: lakeRules, DenySource: config.OriginLocal, ProfileVersion: first.Version})
	report(desk.ID, protocol.AgentReport{AllowSource: config.OriginLocal})
	report(ci.ID, protocol.AgentReport{AllowSource: lakeRules, DenySource: "none", ProfileVersion: first.Version})
	inventories := map[string]protocol.AgentInventory{
		laptop.ID: {Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
			{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 1, Sessions: 3, Reason: config.RefusedNoMatch},
			{GitRemote: "git.example/team/lib", CWD: "/work/lib", CWDs: 1, Sessions: 4, Allowed: true},
			{CWD: "/home/me/notes", CWDs: 1, Sessions: 5, Allowed: true},
			// Refused under a deny rule the stored profile does not
			// hold, on a device that applied it and has deny rules of
			// its own: the device denies it, and no allow rule can let
			// it through.
			{GitRemote: "git.example/team/secret", CWD: "/work/secret", CWDs: 1, Sessions: 6, Reason: config.RefusedByDeny},
			{CWD: "/tmp/x", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch},
		}},
		// desk's config.json sets its allow rules, so the profile's
		// do not reach it.
		desk.ID: {Mode: protocol.InventorySociable, GeneratedAt: now, Projects: []protocol.InventoryProject{
			{GitRemote: "git.example/team/other", CWD: "/src/other", CWDs: 1, Sessions: 8, Reason: config.RefusedNoMatch},
		}},
		// ci is strict: it lists what it uploads and nothing else.
		ci.ID: {Mode: protocol.InventoryStrict, GeneratedAt: now, RefusedSessions: 2, Projects: []protocol.InventoryProject{
			{CWD: "/home/me/notes", CWDs: 1, Sessions: 2, Allowed: true},
		}},
	}
	for id, inv := range inventories {
		if _, err := lake.Catalog.PutDeviceInventory(ctx, id, inv, now); err != nil {
			t.Fatal(err)
		}
	}
	cookie, _ := signIn(t, idp, h)
	csrf := csrfOf(t, h, cookie)
	p, err := lake.Catalog.ProfileByName(ctx, config.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	base := strconvI(p.Revision)
	preview := func(fields map[string]string) string {
		t.Helper()
		w := postForm(h, "/profiles/default/preview", editForm(csrf, base, fields), cookie)
		if w.Code != 200 {
			t.Fatalf("preview: %d %s", w.Code, w.Body)
		}
		return html.UnescapeString(w.Body.String())
	}

	// One owner prefix for the exact rule, and the notes folder gone.
	page := preview(map[string]string{"allow.0.git_remote_prefix": "git.example/team"})
	for _, want := range []string{
		"Admits 1 project",
		"git.example/team/app repository on laptop · 3 sessions",
		"Stops 1 project",
		"/home/me/notes folder on ci, laptop · 7 sessions",
		"ci, fresh send no list of refused projects",
	} {
		if !strings.Contains(squash(page), want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	for _, not := range []string{"git.example/team/secret", "git.example/team/other", "git.example/team/lib repository", "/tmp/x"} {
		if strings.Contains(reachSection(page), not) {
			t.Errorf("preview lists %s", not)
		}
	}

	// The same refusal may be an older profile's, and the project is
	// listed, when the device has no deny rules of its own, has not
	// applied the stored profile, or sent the inventory before it
	// reported applying it.
	for _, c := range []struct {
		name string
		r    protocol.AgentReport
		at   time.Time
	}{
		{"no local deny rules", protocol.AgentReport{AllowSource: lakeRules, DenySource: lakeRules, ProfileVersion: first.Version}, reported},
		{"an older profile", protocol.AgentReport{AllowSource: lakeRules, DenySource: config.OriginLocal + "+" + lakeRules, ProfileVersion: "sha256:older"}, reported},
		{"a report after the inventory", protocol.AgentReport{AllowSource: lakeRules, DenySource: config.OriginLocal, ProfileVersion: first.Version}, now.Add(time.Minute)},
	} {
		if err := lake.Catalog.PutDeviceReport(ctx, laptop.ID, c.r, c.at); err != nil {
			t.Fatal(err)
		}
		if page := preview(map[string]string{"allow.0.git_remote_prefix": "git.example/team"}); !strings.Contains(squash(page), "git.example/team/secret repository on laptop · 6 sessions") {
			t.Errorf("%s: the refused project is not listed", c.name)
		}
	}

	// Removing a deny rule the stored profile holds does admit what it
	// denied, as far as the lake can tell.
	stored = `{"projects":{"allow":[{"git_remote_prefix":"git.example/team"}],"deny":[{"git_remote":"git.example/team/secret"}]}}`
	if p, _, err = lake.Catalog.PutProfile(ctx, config.DefaultProfile, []byte(stored), "test", "", now); err != nil {
		t.Fatal(err)
	}
	base = strconvI(p.Revision)
	page = preview(map[string]string{"allow.0.git_remote_prefix": "git.example/team"})
	if !strings.Contains(squash(page), "git.example/team/secret repository on laptop · 6 sessions") {
		t.Error("removing the stored deny rule does not admit the project it denied")
	}

	// A change outside the project rules lists no projects.
	page = preview(map[string]string{"allow.0.git_remote_prefix": "git.example/team", "deny.0.git_remote": "git.example/team/secret", "debounce": "9s"})
	if strings.Contains(page, "Admits") || strings.Contains(page, "changes whether it uploads") {
		t.Error("a debounce change lists projects")
	}
	// A rule change that flips nothing says so.
	page = preview(map[string]string{"allow.0.git_remote_prefix": "git.example/team", "allow.1.cwd_prefix": "/nowhere", "deny.0.git_remote": "git.example/team/secret"})
	if !strings.Contains(page, "changes whether it uploads") {
		t.Error("a change that flips nothing does not say so")
	}
}

var spaces = regexp.MustCompile(`\s+`)

// squash is page with its tags dropped and its whitespace folded, as a
// reader sees the text.
func squash(page string) string {
	return spaces.ReplaceAllString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(page, " "), " ")
}

// reachSection is the part of a preview page that lists projects.
func reachSection(page string) string {
	i := strings.Index(page, `id="preview-heading"`)
	j := strings.Index(page, `<pre class="diff">`)
	if i < 0 || j < i {
		return ""
	}
	return page[i:j]
}
