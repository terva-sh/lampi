package catalog

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func reviewDevices(t *testing.T, c *Catalog, now time.Time, names ...string) []Device {
	t.Helper()
	var entries []TokenEntry
	for i, n := range names {
		entries = append(entries, TokenEntry{Hash: strings.Repeat(string(rune('a'+i)), 64), Name: n})
	}
	ds, err := c.SyncTokenFile(context.Background(), entries, now)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func putInventory(t *testing.T, c *Catalog, id string, at time.Time, mode string, ps ...protocol.InventoryProject) {
	t.Helper()
	inv := protocol.AgentInventory{Mode: mode, GeneratedAt: at, Projects: ps}
	for _, p := range ps {
		if !p.Allowed {
			inv.RefusedSessions += p.Sessions
			inv.RefusedBytes += p.Bytes
		}
	}
	if kept, err := c.PutDeviceInventory(context.Background(), id, inv, at); err != nil || !kept {
		t.Fatalf("put inventory: kept=%v err=%v", kept, err)
	}
}

func queuedEvents(t *testing.T, c *Catalog, kind string) []string {
	t.Helper()
	rows, err := c.db.Query(`SELECT event FROM audit_outbox ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(e, `"kind":"`+kind+`"`) {
			out = append(out, e)
		}
	}
	return out
}

var (
	appRemote = protocol.InventoryProject{GitRemote: "git.example/team/app", CWD: "/work/app", CWDs: 1, Sessions: 3, Bytes: 300, Reason: config.RefusedNoMatch}
	scratch   = protocol.InventoryProject{CWD: "/home/me/scratch", CWDs: 1, Sessions: 2, Bytes: 20, Reason: config.RefusedNoMatch}
	noCWD     = protocol.InventoryProject{Sessions: 1, Reason: config.RefusedNoCWD}
	allowedOK = protocol.InventoryProject{CWD: "/work/ok", CWDs: 1, Sessions: 1, Allowed: true}
)

// TKT-01M3N8FHSG: each kept inventory marks its projects seen by the
// lake's clock, keeping the first time; a late inventory the lake does
// not keep marks nothing.
func TestInventoriesRecordWhenEachProjectWasFirstSeen(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	t1 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	laptop := reviewDevices(t, c, t1, "laptop")[0]
	putInventory(t, c, laptop.ID, t1, protocol.InventorySociable, appRemote, scratch, noCWD, allowedOK)

	t2 := t1.Add(time.Hour)
	later := protocol.InventoryProject{CWD: "/home/me/later", CWDs: 1, Sessions: 1, Reason: config.RefusedNoMatch}
	putInventory(t, c, laptop.ID, t2, protocol.InventorySociable, appRemote, later)
	// Generated before the one stored: not kept, and seen nowhere.
	stale := protocol.AgentInventory{Mode: protocol.InventorySociable, GeneratedAt: t1.Add(time.Minute),
		Projects: []protocol.InventoryProject{{CWD: "/stale", Reason: config.RefusedNoMatch}}}
	if kept, err := c.PutDeviceInventory(ctx, laptop.ID, stale, t2.Add(time.Minute)); err != nil || kept {
		t.Fatalf("stale inventory kept=%v err=%v", kept, err)
	}

	first, err := c.firstSeen(ctx, laptop.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := map[ProjectKey]time.Time{
		{KeyGitRemote, "git.example/team/app"}: t1,
		{KeyCWD, "/home/me/scratch"}:           t1,
		{KeyCWD, "/work/ok"}:                   t1,
		{KeyCWD, "/home/me/later"}:             t2,
	}
	if len(first) != len(want) {
		t.Fatalf("sightings %v, want %v", first, want)
	}
	for k, at := range want {
		if !first[k].Equal(at) {
			t.Errorf("%s first seen %v, want %v", k, first[k], at)
		}
	}
	var last int64
	if err := c.db.QueryRow(`SELECT last_seen_ns FROM project_sightings WHERE device_id=? AND key=?`, laptop.ID, "git.example/team/app").Scan(&last); err != nil || last != t2.UnixNano() {
		t.Fatalf("last seen %d %v, want %d", last, err, t2.UnixNano())
	}
}

// TKT-01M3N8FHSG: the queue groups refused projects lake-wide by key,
// newest first seen first, carries hides, gives strict devices' totals,
// and leaves out revoked devices and projects no rule could name.
func TestReviewQueueGroupsRefusedProjectsAcrossDevices(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	t1 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	ds := reviewDevices(t, c, t1, "laptop", "desk", "locked", "old")
	laptop, desk, locked, old := ds[0], ds[1], ds[2], ds[3]
	if _, err := c.SetDeviceProfileByID(ctx, desk.ID, "ci", "ci", "test", t1); err != nil {
		t.Fatal(err)
	}
	putInventory(t, c, laptop.ID, t1, protocol.InventorySociable, appRemote, noCWD, allowedOK)
	// The same repository by its ssh remote, in another checkout.
	deskApp := appRemote
	deskApp.GitRemote, deskApp.CWD, deskApp.Sessions, deskApp.Bytes = "git@git.example:team/app.git", "/src/app", 4, 400
	deskApp.Newest = t1.Add(-time.Hour)
	putInventory(t, c, desk.ID, t1.Add(2*time.Hour), protocol.InventorySociable, deskApp, scratch)
	putInventory(t, c, locked.ID, t1, protocol.InventoryStrict, protocol.InventoryProject{CWD: "/x", Sessions: 5, Bytes: 50, Reason: config.RefusedNoMatch})
	putInventory(t, c, old.ID, t1, protocol.InventorySociable, protocol.InventoryProject{CWD: "/gone", Sessions: 1, Reason: config.RefusedNoMatch})
	if _, err := c.RevokeDeviceByID(ctx, old.ID, "test", t1); err != nil {
		t.Fatal(err)
	}

	q, err := c.ReviewQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, p := range q.Projects {
		keys = append(keys, p.Key.String())
	}
	// scratch was first seen on desk at t1+2h, after the app on laptop.
	if want := []string{"cwd /home/me/scratch", "git_remote git.example/team/app"}; !slices.Equal(keys, want) {
		t.Fatalf("keys %v, want %v", keys, want)
	}
	app := q.Projects[1]
	if len(app.Sightings) != 2 || app.Sightings[0].DeviceName != "desk" || app.Sightings[1].DeviceName != "laptop" {
		t.Fatalf("app sightings %+v", app.Sightings)
	}
	if app.Sessions != 7 || app.Bytes != 700 || !app.FirstSeen.Equal(t1) || !app.Newest.Equal(deskApp.Newest) {
		t.Fatalf("app %+v", app)
	}
	if app.Sightings[0].Profile != "ci" || app.Sightings[1].Profile != "" {
		t.Fatalf("profiles %q %q", app.Sightings[0].Profile, app.Sightings[1].Profile)
	}
	if len(q.Strict) != 1 || q.Strict[0] != (StrictRefusals{locked.ID, "locked", 5, 50}) {
		t.Fatalf("strict %+v", q.Strict)
	}

	if _, err := c.HideProjects(ctx, []ProjectKey{app.Key}, "oidc:ops", "vendored", t1); err != nil {
		t.Fatal(err)
	}
	q, _ = c.ReviewQueue(ctx)
	if h := q.Projects[1].Hidden; h == nil || h.By != "oidc:ops" || h.Note != "vendored" {
		t.Fatalf("hidden %+v", h)
	}
	if q.Projects[0].Hidden != nil {
		t.Fatal("scratch reads hidden")
	}
}

// TKT-01M3N8FHSG: hides are batched in one transaction, audited once
// each, and refused whole when one key is not a key.
func TestHideAndUnhideProjects(t *testing.T) {
	c, _ := openTemp(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	app, tmp := ProjectKey{KeyGitRemote, "git.example/team/app"}, ProjectKey{KeyCWD, "/tmp/x"}

	got, err := c.HideProjects(ctx, []ProjectKey{app, tmp}, "oidc:ops", "", now)
	if err != nil || len(got) != 2 {
		t.Fatalf("hide: %v %v", got, err)
	}
	// Hiding again changes nothing and keeps the first hide.
	if got, err := c.HideProjects(ctx, []ProjectKey{app}, "oidc:other", "again", now.Add(time.Hour)); err != nil || len(got) != 0 {
		t.Fatalf("hide again: %v %v", got, err)
	}
	for _, bad := range []ProjectKey{{"cwd_prefix", "/x"}, {KeyCWD, ""}, {KeyCWD, "/a\nb"}, {KeyCWD, strings.Repeat("x", maxProjectKey+1)}} {
		if _, err := c.HideProjects(ctx, []ProjectKey{{KeyCWD, "/fine"}, bad}, "oidc:ops", "", now); !errors.Is(err, ErrProjectKey) {
			t.Fatalf("%q: %v", bad.String(), err)
		}
	}
	for _, note := range []string{strings.Repeat("n", MaxHideNote+1), "two\nlines"} {
		if _, err := c.HideProjects(ctx, []ProjectKey{{KeyCWD, "/fine"}}, "oidc:ops", note, now); !errors.Is(err, ErrHideNote) {
			t.Fatalf("note %.20q: %v", note, err)
		}
	}
	if _, err := c.HideProjects(ctx, []ProjectKey{{KeyCWD, "/fine"}}, "oidc:ops", strings.Repeat("é", MaxHideNote), now); err != nil {
		t.Fatalf("a note of %d characters: %v", MaxHideNote, err)
	}
	if _, err := c.UnhideProjects(ctx, []ProjectKey{{KeyCWD, "/fine"}}, "oidc:ops", now); err != nil {
		t.Fatal(err)
	}
	hides, err := c.HiddenProjects(ctx)
	if err != nil || len(hides) != 2 {
		t.Fatalf("hides %+v %v; a refused batch must write nothing", hides, err)
	}
	for _, h := range hides {
		if h.By != "oidc:ops" || h.Note != "" || !h.At.Equal(now) {
			t.Fatalf("hide %+v", h)
		}
	}
	if n := len(queuedEvents(t, c, audit.ProjectHidden)); n != 3 {
		t.Fatalf("%d project.hidden events, want 3: two, and the note at the limit", n)
	}

	if got, err := c.UnhideProjects(ctx, []ProjectKey{app, {KeyCWD, "/never"}}, "oidc:ops", now); err != nil || !slices.Equal(got, []ProjectKey{app}) {
		t.Fatalf("unhide: %v %v", got, err)
	}
	if ev := queuedEvents(t, c, audit.ProjectUnhidden); len(ev) != 2 || !strings.Contains(ev[1], "git_remote git.example/team/app") {
		t.Fatalf("unhidden events %v", ev)
	}
	if hides, _ := c.HiddenProjects(ctx); len(hides) != 1 || hides[0].Key != tmp {
		t.Fatalf("after unhide %+v", hides)
	}
}

// TKT-01M3N8FHSG: upgrading seeds sightings from the inventories the
// lake already holds, at the time each arrived, and records when
// sightings began.
func TestProjectReviewMigrationSeedsSightings(t *testing.T) {
	c, path := openTemp(t)
	ctx := context.Background()
	t1 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	laptop := reviewDevices(t, c, t1, "laptop")[0]
	putInventory(t, c, laptop.ID, t1, protocol.InventorySociable, appRemote, scratch)
	if _, err := c.db.Exec(`DROP TABLE project_sightings; DROP TABLE hidden_projects; DELETE FROM lake_meta WHERE key='sightings_since'; PRAGMA user_version = ` + strconv.Itoa(len(migrations)-1)); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Second)
	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	first, err := c.firstSeen(ctx, laptop.ID)
	if err != nil || len(first) != 2 || !first[ProjectKey{KeyCWD, "/home/me/scratch"}].Equal(t1) {
		t.Fatalf("seeded %v %v", first, err)
	}
	if since, err := c.SightingsSince(ctx); err != nil || since.Before(before) {
		t.Fatalf("sightings since %v %v", since, err)
	}
}
