package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryScopesAndRoutingEvidence(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	one, other := newSession(t, c, "reviewer"), newSession(t, c, "development")
	work, err := c.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := c.CreateBay(ctx, "private-client", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []Membership{{SessionUID: one, Bay: work.ID}, {SessionUID: one, Bay: secret.ID}, {SessionUID: other, Bay: secret.ID}} {
		m.Actor, m.Via = "admin", ViaCLI
		if _, err := c.AddToBay(ctx, m, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, uid := range []string{one, other} {
		_, err := c.db.Exec(`UPDATE sessions SET project_id='shared-repo',manifest_json=json_set(manifest_json,'$.project.cwd','/home/maë/reviews/run','$.project.cwd_hash','directory-hash','$.project.git_remote','git@example:repo.git','$.private_fixture','must-not-appear') WHERE session_uid=?`, uid)
		if err != nil {
			t.Fatal(err)
		}
	}
	scope := InBays([]string{work.ID})
	page, err := c.DiscoverSessions(ctx, scope, DiscoveryRequest{CWDPrefix: "/home/maë/reviews", Machine: "machine-a"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("sessions %+v %v", page, err)
	}
	s := page.Items[0]
	if s.UID != one || s.CWD != "/home/maë/reviews/run" || s.CWDHash != "directory-hash" || s.GitRemote != "git@example:repo.git" || s.Workflow != nil || s.MachineCount != 1 || len(s.Bays) != 1 || s.Bays[0].ID != work.ID {
		t.Fatalf("routing evidence %+v", s)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "private-client") || strings.Contains(string(encoded), "must-not-appear") {
		t.Fatalf("private metadata exposed: %s", encoded)
	}
	projects, err := c.DiscoverProjects(ctx, scope, DiscoveryRequest{})
	if err != nil || len(projects.Items) != 1 || projects.Items[0].Sessions != 1 {
		t.Fatalf("projects %+v %v", projects, err)
	}
	bays, err := c.DiscoverBays(ctx, scope, DiscoveryRequest{})
	if err != nil || len(bays.Items) != 1 || bays.Items[0].Sessions != 1 {
		t.Fatalf("bays %+v %v", bays, err)
	}
	for _, uid := range []string{other, "unknown"} {
		if _, err := c.DiscoverSession(ctx, scope, uid); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("inaccessible ID: %v", err)
		}
	}
	for name, scope := range map[string]Scope{"none": {}, "empty sessions": AllBays().OnlySessions(nil), "wrong session": scope.OnlySessions([]string{other})} {
		p, err := c.DiscoverSessions(ctx, scope, DiscoveryRequest{})
		b, berr := c.DiscoverBays(ctx, scope, DiscoveryRequest{})
		if err != nil || berr != nil || len(p.Items) != 0 || len(b.Items) != 0 {
			t.Fatalf("%s: sessions %+v bays %+v errors %v %v", name, p, b, err, berr)
		}
	}
	// A fixed-session token must not enumerate bays unrelated to its session.
	bays, err = c.DiscoverBays(ctx, AllBays().OnlySessions([]string{other}), DiscoveryRequest{})
	if err != nil || len(bays.Items) != 2 {
		t.Fatalf("fixed-session bays %+v %v", bays, err)
	}
	for _, bay := range bays.Items {
		if bay.ID == work.ID {
			t.Fatal("unrelated bay exposed")
		}
	}
	// A prefix is a directory boundary, not a substring match.
	page, err = c.DiscoverSessions(ctx, scope, DiscoveryRequest{CWDPrefix: "/home/maë/review"})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("prefix boundary %+v %v", page, err)
	}
}

func TestDiscoveryPagingAndScopeBoundCursors(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	for _, native := range []string{"reviewer", "rater", "development"} {
		uid := newSession(t, c, native)
		project := strings.Repeat(native, 40)
		if _, err := c.db.Exec(`UPDATE sessions SET project_id=? WHERE session_uid=?`, project, uid); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	r := DiscoveryRequest{Limit: 1}
	for {
		p, err := c.DiscoverSessions(ctx, AllBays(), r)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range p.Items {
			if seen[s.UID] {
				t.Fatal("repeated session")
			}
			seen[s.UID] = true
		}
		if p.NextCursor == "" {
			break
		}
		r.Cursor = p.NextCursor
	}
	if len(seen) != 3 {
		t.Fatal("incomplete session enumeration")
	}
	first, err := c.DiscoverSessions(ctx, AllBays(), DiscoveryRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []DiscoveryRequest{{Limit: 2, Cursor: first.NextCursor}, {Limit: 1, Harness: "claude", Cursor: first.NextCursor}, {Limit: 1, Cursor: first.NextCursor + "bad"}} {
		if _, err := c.DiscoverSessions(ctx, AllBays(), change); !errors.Is(err, ErrPage) {
			t.Fatalf("changed/invalid cursor accepted %v", err)
		}
	}
	if _, err := c.DiscoverSessions(ctx, InBays([]string{DefaultBayID}), DiscoveryRequest{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrPage) {
		t.Fatalf("cross-scope cursor accepted %v", err)
	}
	if _, err := c.DiscoverProjects(ctx, AllBays(), DiscoveryRequest{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, ErrPage) {
		t.Fatal("cross-collection cursor accepted")
	}
	seenProjects := map[string]bool{}
	r = DiscoveryRequest{Limit: 1}
	for {
		p, err := c.DiscoverProjects(ctx, AllBays(), r)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range p.Items {
			seenProjects[item.ID] = true
		}
		if p.NextCursor == "" {
			break
		}
		r.Cursor = p.NextCursor
	}
	if len(seenProjects) != 3 {
		t.Fatal("long project IDs did not page")
	}
	for _, bad := range []DiscoveryRequest{{Limit: 201}, {Limit: -1}, {CWDPrefix: "relative"}, {Project: "x", Unlinked: true}} {
		if _, err := c.DiscoverSessions(ctx, AllBays(), bad); !errors.Is(err, ErrPage) {
			t.Fatalf("invalid filter accepted %v", err)
		}
	}
}

func TestDiscoveryClippedEvidenceAndOversizedProjectPaging(t *testing.T) {
	c, _ := openTemp(t)
	uid := newSession(t, c, "large")
	other := newSession(t, c, "other")
	huge := strings.Repeat("界", 5000)
	if _, err := c.db.Exec(`UPDATE sessions SET project_id=?, manifest_json=json_set(manifest_json,'$.project.cwd',?,'$.project.cwd_hash',?,'$.project.git_remote',?) WHERE session_uid=?`, huge, "/"+huge, strings.Repeat("h", 129), huge, uid); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err := c.db.Exec(`INSERT INTO provenance(session_uid,machine_id,sha256,relpath,ingested_at) VALUES(?,?,'head','other','2026-09-26T00:00:00Z')`, uid, fmt.Sprintf("machine-extra-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	item, err := c.DiscoverSession(t.Context(), AllBays(), uid)
	if err != nil || !item.MetadataTruncated || !item.MachinesTruncated || len(item.Machines) != 5 || item.MachineCount != 8 || len([]rune(item.CWD)) != 4096 {
		t.Fatalf("clipped %+v %v", item, err)
	}
	if _, err := c.db.Exec(`UPDATE sessions SET project_id='ordinary' WHERE session_uid=?`, other); err != nil {
		t.Fatal(err)
	}
	seen := 0
	clipped := 0
	r := DiscoveryRequest{Limit: 1}
	for {
		p, err := c.DiscoverProjects(t.Context(), AllBays(), r)
		if err != nil {
			t.Fatal(err)
		}
		for _, project := range p.Items {
			seen++
			if project.IDTruncated {
				clipped++
				if len([]rune(project.ID)) != 512 {
					t.Fatal("unbounded project preview")
				}
			}
		}
		if p.NextCursor == "" {
			break
		}
		r.Cursor = p.NextCursor
	}
	if seen != 2 || clipped != 1 {
		t.Fatalf("projects seen=%d clipped=%d", seen, clipped)
	}
}
