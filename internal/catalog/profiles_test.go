package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
)

func TestPutProfileKeepsRevisions(t *testing.T) {
	c, path := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	// Spacing and key order are not part of the document.
	p, changed, err := c.PutProfile(ctx, "ci", []byte(` { "agent": {"debounce": "5s"} } `), "op", "first", now)
	if err != nil || !changed {
		t.Fatalf("put: %v %v", changed, err)
	}
	want, _ := config.ParseProfile([]byte(`{"agent":{"debounce":"5s"}}`))
	if p.Version != want.Version() || p.Document != `{"agent":{"debounce":"5s"}}` {
		t.Fatalf("stored %s %s, want version %s", p.Document, p.Version, want.Version())
	}
	// The same content again is no revision and no event.
	if again, changed, err := c.PutProfile(ctx, "ci", []byte(`{"agent":{"debounce":"5s"}}`), "op", "", now); err != nil || changed || again.Revision != p.Revision {
		t.Fatalf("unchanged put: %+v %v %v", again, changed, err)
	}
	p2, changed, err := c.PutProfile(ctx, "ci", []byte(`{"agent":{"debounce":"9s"}}`), "op2", "", now.Add(time.Minute))
	if err != nil || !changed || p2.Revision == p.Revision {
		t.Fatalf("second put: %+v %v %v", p2, changed, err)
	}
	got, err := c.ProfileByName(ctx, "ci")
	if err != nil || got.Version != p2.Version || got.UpdatedBy != "op2" || got.Config.Agent.Debounce != "9s" {
		t.Fatalf("read back: %+v %v", got, err)
	}
	revs, err := c.ProfileRevisions(ctx, "ci")
	if err != nil || len(revs) != 2 || revs[0].ID != p2.Revision || revs[1].Note != "first" || revs[1].CreatedBy != "op" {
		t.Fatalf("revisions: %+v %v", revs, err)
	}
	if err := c.FlushAudit(ctx, filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(audit.Path(filepath.Dir(path)))
	if n := strings.Count(string(raw), `"kind":"profile.put"`); n != 2 {
		t.Fatalf("%d profile.put lines, want 2:\n%s", n, raw)
	}
	if !strings.Contains(string(raw), "profile=ci revision=") {
		t.Fatalf("event does not name the revision:\n%s", raw)
	}
}

func TestPutProfileRefusesWhatAnAgentWould(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	for name, raw := range map[string]string{
		"root":        `{"harnesses":{"terva":{"root":""}}}`,
		"upload_hits": `{"redaction":{"UPLOAD_HITS":false}}`,
		"unknown":     `{"nope":1}`,
		"trailing":    `{} {}`,
		"debounce":    `{"agent":{"debounce":"soon"}}`,
	} {
		if _, _, err := c.PutProfile(ctx, "ci", []byte(raw), "op", "", time.Now()); err == nil {
			t.Errorf("%s: accepted %s", name, raw)
		}
	}
	if _, _, err := c.PutProfile(ctx, "Bad Name", []byte(`{}`), "op", "", time.Now()); !errors.Is(err, ErrProfileName) {
		t.Errorf("bad name: %v", err)
	}
	if list, err := c.Profiles(ctx); err != nil || len(list) != 0 {
		t.Fatalf("a refused put stored %+v %v", list, err)
	}
	if n, err := c.PendingAudit(ctx); err != nil || n != 0 {
		t.Fatalf("a refused put queued %d events: %v", n, err)
	}
}

func TestDeleteProfile(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"default", "ci"} {
		if _, _, err := c.PutProfile(ctx, name, []byte(`{}`), "op", "", now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.DeleteProfile(ctx, "default", "op", "", now); !errors.Is(err, ErrDefaultProfile) {
		t.Fatalf("deleted default: %v", err)
	}
	if _, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}, {Hash: strings.Repeat("b", 64), Name: "desk"}}, now); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"laptop", "desk"} {
		if _, err := c.SetDeviceProfile(ctx, d, "ci", "ci", "op", now); err != nil {
			t.Fatal(err)
		}
	}
	_, err := c.DeleteProfile(ctx, "ci", "op", "", now)
	if !errors.Is(err, ErrProfileInUse) || !strings.Contains(err.Error(), "desk, laptop") {
		t.Fatalf("deleted a profile in use: %v", err)
	}
	// A revoked device no longer holds the profile.
	for _, d := range []string{"laptop", "desk"} {
		if _, err := c.RevokeDevice(ctx, d, "op", now); err != nil {
			t.Fatal(err)
		}
	}
	r, err := c.DeleteProfile(ctx, "ci", "op", "retired", now)
	if err != nil || !r.Deleted || r.ID == 0 {
		t.Fatalf("delete: %+v %v", r, err)
	}
	if _, err := c.ProfileByName(ctx, "ci"); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("deleted profile still read: %v", err)
	}
	if _, err := c.DeleteProfile(ctx, "ci", "op", "", now); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("second delete: %v", err)
	}
	revs, err := c.ProfileRevisions(ctx, "ci")
	if err != nil || len(revs) != 2 || !revs[0].Deleted || revs[0].Note != "retired" || revs[1].Deleted {
		t.Fatalf("revisions: %+v %v", revs, err)
	}
	// Saving the name again continues its history.
	p, _, err := c.PutProfile(ctx, "ci", []byte(`{}`), "op", "", now)
	if err != nil || p.Revision <= r.ID {
		t.Fatalf("put after delete: %+v %v", p, err)
	}
}

// TKT-01M3M7M0ZY: an editor's save or delete names the revision it read,
// and loses to a write that landed since.
func TestProfileWritesGuardedByRevision(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	if _, _, err := c.PutProfileIf(ctx, "ci", []byte(`{}`), "op", "", 3, now); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("new profile at revision 3: %v", err)
	}
	p1, changed, err := c.PutProfileIf(ctx, "ci", []byte(`{}`), "op", "", 0, now)
	if err != nil || !changed {
		t.Fatalf("create: %v %v", changed, err)
	}
	if _, _, err := c.PutProfileIf(ctx, "ci", []byte(`{"agent":{"debounce":"5s"}}`), "op", "", 0, now); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("create twice: %v", err)
	}
	p2, _, err := c.PutProfileIf(ctx, "ci", []byte(`{"agent":{"debounce":"5s"},"projects":{"allow":[{"cwd_prefix":"/w"}]}}`), "op", "", p1.Revision, now)
	if err != nil {
		t.Fatal(err)
	}
	// A second editor who read p1 loses.
	if _, _, err := c.PutProfileIf(ctx, "ci", []byte(`{}`), "op2", "", p1.Revision, now); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("stale save: %v", err)
	}
	if _, err := c.DeleteProfileIf(ctx, "ci", "op2", "", p1.Revision, now); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("stale delete: %v", err)
	}
	if _, err := c.DeleteProfileIf(ctx, "ci", "op", "", p2.Revision, now); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := ChangedProfileFields(p1.Config, p2.Config); strings.Join(got, ",") != "projects.allow,agent" {
		t.Fatalf("changed %v", got)
	}
}

func TestRecentProfileRevisions(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	for _, d := range []string{"1s", "2s", "3s"} {
		if _, _, err := c.PutProfile(ctx, "ci", []byte(`{"agent":{"debounce":"`+d+`"}}`), "op", d, now); err != nil {
			t.Fatal(err)
		}
	}
	two, err := c.RecentProfileRevisions(ctx, "ci", 2)
	if err != nil || len(two) != 2 || two[0].Note != "3s" || two[1].Note != "2s" {
		t.Fatalf("newest two: %+v %v", two, err)
	}
	if all, err := c.ProfileRevisions(ctx, "ci"); err != nil || len(all) != 3 {
		t.Fatalf("all: %d %v", len(all), err)
	}
}

// Review of #101: an editor who opened a name that was not stored loses
// to a create and delete of that name in between.
func TestCreateGuardedAcrossCreateAndDelete(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	opened, err := c.LatestProfileRevision(ctx, "ci")
	if err != nil || opened != 0 {
		t.Fatalf("never saved: %d %v", opened, err)
	}
	p, _, err := c.PutProfile(ctx, "ci", []byte(`{}`), "other", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteProfileIf(ctx, "ci", "other", "", p.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.PutProfileIf(ctx, "ci", []byte(`{}`), "op", "", opened, now); !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("stale create: %v", err)
	}
	latest, err := c.LatestProfileRevision(ctx, "ci")
	if err != nil || latest <= p.Revision {
		t.Fatalf("after delete: %d %v", latest, err)
	}
	if _, err := c.DeleteProfileIf(ctx, "ci", "op", "", latest, now); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("delete of an absent profile read as absent: %v", err)
	}
	if _, err := c.DeleteProfileIf(ctx, "never", "op", "", 7, now); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("delete of a name never saved: %v", err)
	}
	if _, changed, err := c.PutProfileIf(ctx, "ci", []byte(`{}`), "op", "", latest, now); err != nil || !changed {
		t.Fatalf("create after reading the deletion: %v %v", changed, err)
	}
}

// TKT-01M3N8FHZR: a batch saves every profile against the revision its
// caller read, or none of them.
func TestPutProfilesIfIsAllOrNothing(t *testing.T) {
	c, _ := openTemp(t)
	ctx := t.Context()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	ci, _, err := c.PutProfile(ctx, "ci", []byte(`{}`), "test", "", now)
	if err != nil {
		t.Fatal(err)
	}
	def, err := c.LatestProfileRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	allowA := []byte(`{"projects":{"allow":[{"cwd_prefix":"/a"}]}}`)

	// One stale base refuses the whole batch.
	_, err = c.PutProfilesIf(ctx, []ProfileWrite{{"default", allowA, def}, {"ci", allowA, ci.Revision + 7}}, "oidc:ops", "both", now)
	if !errors.Is(err, ErrProfileChanged) {
		t.Fatalf("stale: %v", err)
	}
	if _, err := c.ProfileByName(ctx, "default"); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("a refused batch saved default: %v", err)
	}
	if _, err := c.PutProfilesIf(ctx, []ProfileWrite{{"ci", allowA, ci.Revision}, {"ci", allowA, ci.Revision}}, "oidc:ops", "", now); err == nil {
		t.Fatal("a profile written twice in one batch was accepted")
	}

	ps, err := c.PutProfilesIf(ctx, []ProfileWrite{{"default", allowA, def}, {"ci", allowA, ci.Revision}}, "oidc:ops", "both", now)
	if err != nil || len(ps) != 2 || ps[0].Name != "default" || ps[1].Revision <= ci.Revision {
		t.Fatalf("batch: %+v %v", ps, err)
	}
	for _, name := range []string{"default", "ci"} {
		p, err := c.ProfileByName(ctx, name)
		if err != nil || len(p.Config.Projects.Allow) != 1 {
			t.Fatalf("%s: %+v %v", name, p, err)
		}
		revs, _ := c.RecentProfileRevisions(ctx, name, 1)
		if len(revs) != 1 || revs[0].Note != "both" || revs[0].CreatedBy != "oidc:ops" {
			t.Fatalf("%s revision %+v", name, revs)
		}
	}
	// Writing what is stored changes nothing and saves no revision.
	again, err := c.PutProfilesIf(ctx, []ProfileWrite{{"ci", allowA, ps[1].Revision}}, "oidc:ops", "", now)
	if err != nil || again[0].Revision != ps[1].Revision {
		t.Fatalf("unchanged: %+v %v", again, err)
	}
}
