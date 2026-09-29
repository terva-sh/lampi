package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/protocol"
)

// newSession stores one session with its own native id and returns its
// uid. transcriptPoster reuses one native id for every post.
func newSession(t *testing.T, c *Catalog, native string) string {
	t.Helper()
	body := []byte(`{"session":"` + native + `"}` + "\n")
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/" + native + ".jsonl", Size: int64(len(body)), SHA256: digestHex(body),
		}},
	}
	ack, err := c.Ingest(context.Background(), m, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, memBlobs{digestHex(body): body})
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

func TestBayMigrationPutsEverySessionAndDeviceInDefault(t *testing.T) {
	ctx := context.Background()
	c, path := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	a := p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n"))
	b := p.mustPost("machine-a", "sessions/bbbb/sess-2.jsonl", []byte("{\"n\":2}\n"))
	devs, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, p.now)
	if err != nil {
		t.Fatal(err)
	}
	// Back to the schema before bays: a lake with sessions and a device.
	if _, err := c.db.Exec(`DROP TABLE bays; DROP TABLE bay_aliases; DROP TABLE session_bays; DROP TABLE session_bay_requests; DROP TABLE bay_grants; PRAGMA user_version = 17`); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if m := c.Migrated(); m.From != 17 || len(m.Steps) != 1 || m.Steps[0] != "migrateBays" {
		t.Fatalf("migration: %+v", m)
	}
	for _, uid := range []string{a.SessionUID, b.SessionUID} {
		if got, err := c.SessionBays(ctx, uid); err != nil || !reflect.DeepEqual(got, []string{DefaultBayID}) {
			t.Fatalf("session %s bays %v err=%v", uid, got, err)
		}
	}
	grants, err := c.Grants(ctx, PrincipalDevice, devs[0].ID)
	if err != nil || len(grants) != 1 || grants[0].BayID != DefaultBayID || grants[0].Permission != PermWrite {
		t.Fatalf("device grants %+v err=%v", grants, err)
	}
	bays, err := c.Bays(ctx)
	if err != nil || len(bays) != 1 || !bays[0].Default || bays[0].Name != DefaultBayName || bays[0].Disabled {
		t.Fatalf("bays %+v err=%v", bays, err)
	}
}

func TestNewSessionsAndDevicesLandInDefault(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Now()}
	ack := p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n"))
	if got, _ := c.SessionBays(ctx, ack.SessionUID); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("new session bays %v", got)
	}
	// An append moves the head and adds no second membership.
	p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n{\"n\":2}\n"))
	if got, _ := c.SessionBays(ctx, ack.SessionUID); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("after append bays %v", got)
	}
	fromFile, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("b", 64)}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	fromAllow, err := c.AllowTokens(ctx, []TokenEntry{{Hash: strings.Repeat("c", 64)}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range append(fromFile, fromAllow...) {
		if g, _ := c.Grants(ctx, PrincipalDevice, d.ID); len(g) != 1 || g[0].BayID != DefaultBayID || g[0].Permission != PermWrite {
			t.Fatalf("new %s device grants %+v", d.Source, g)
		}
	}
}

func TestBayNames(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	for _, bad := range []string{"", "Work", "-x", "has space", "under_score", strings.Repeat("a", 64)} {
		if _, err := c.CreateBay(ctx, bad, "admin", time.Now()); !errors.Is(err, ErrBayName) {
			t.Fatalf("name %q: err=%v", bad, err)
		}
	}
	work, err := c.CreateBay(ctx, "client-x", "admin", time.Now())
	if err != nil || !strings.HasPrefix(work.ID, "bay_") {
		t.Fatalf("create %+v err=%v", work, err)
	}
	for _, taken := range []string{"client-x", DefaultBayName} {
		if _, err := c.CreateBay(ctx, taken, "admin", time.Now()); !errors.Is(err, ErrBayTaken) {
			t.Fatalf("name %q: err=%v", taken, err)
		}
	}
	for _, ref := range []string{work.ID, "client-x"} {
		if b, err := c.ResolveBay(ctx, ref); err != nil || b.ID != work.ID {
			t.Fatalf("resolve %q: %+v err=%v", ref, b, err)
		}
	}
	if _, err := c.ResolveBay(ctx, "nope"); !errors.Is(err, ErrNoBay) {
		t.Fatalf("resolve missing: %v", err)
	}
	if got := queuedEvents(t, c, audit.BayCreated); len(got) != 1 {
		t.Fatalf("audit %v", got)
	}
}

// TestANameAndAnAliasNeverMeet is review 1392: the schema, not only
// the writers, keeps a bay's name from also being another bay's alias,
// in both directions, so a reference resolves to one bay.
func TestANameAndAnAliasNeverMeet(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	work, err := c.CreateBay(ctx, "work", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(ctx, `INSERT INTO bay_aliases(alias, bay_id) VALUES(?, ?)`, DefaultBayName, work.ID); err == nil {
		t.Fatal("an alias took the default bay's name")
	}
	if _, err := c.db.ExecContext(ctx, `INSERT INTO bay_aliases(alias, bay_id) VALUES('old-work', ?)`, work.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE bay_aliases SET alias=? WHERE alias='old-work'`, DefaultBayName); err == nil {
		t.Fatal("an alias was renamed to the default bay's name")
	}
	if _, err := c.CreateBay(ctx, "old-work", "admin", time.Now()); !errors.Is(err, ErrBayTaken) {
		t.Fatalf("create over an alias: %v", err)
	}
	if _, err := c.db.ExecContext(ctx, `INSERT INTO bays(id, name, created_at, created_by) VALUES('bay_x', 'old-work', '', 't')`); err == nil {
		t.Fatal("a bay took an alias as its name")
	}
	if _, err := c.db.ExecContext(ctx, `UPDATE bays SET name='old-work' WHERE id=?`, DefaultBayID); err == nil {
		t.Fatal("a rename took an alias")
	}
	if b, err := c.ResolveBay(ctx, "old-work"); err != nil || b.ID != work.ID {
		t.Fatalf("resolve alias: %+v err=%v", b, err)
	}
}

func TestMembershipIsAuditedAndNeverEmpty(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Now()}
	uid := p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n")).SessionUID
	work, err := c.CreateBay(ctx, "work", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	m := Membership{SessionUID: uid, Bay: "work", Actor: "admin", Via: ViaCLI, Reason: "sorting"}
	if added, err := c.AddToBay(ctx, m, time.Now()); err != nil || !added {
		t.Fatalf("add added=%v err=%v", added, err)
	}
	if added, err := c.AddToBay(ctx, m, time.Now()); err != nil || added {
		t.Fatalf("second add added=%v err=%v", added, err)
	}
	want := []string{DefaultBayID, work.ID}
	if DefaultBayID > work.ID {
		want = []string{work.ID, DefaultBayID}
	}
	if got, _ := c.SessionBays(ctx, uid); !reflect.DeepEqual(got, want) {
		t.Fatalf("bays %v want %v", got, want)
	}
	// Out of the inbox, into work only.
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: DefaultBayName, Actor: "admin", Via: ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.SessionBays(ctx, uid); !reflect.DeepEqual(got, []string{work.ID}) {
		t.Fatalf("after sorting %v", got)
	}
	// Taking the last bay puts the session back in the inbox.
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: "work", Actor: "admin", Via: ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.SessionBays(ctx, uid); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("after last removal %v", got)
	}
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: DefaultBayName, Actor: "admin", Via: ViaCLI}, time.Now()); !errors.Is(err, ErrLastBay) {
		t.Fatalf("removing the only default: %v", err)
	}
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: "work", Actor: "admin", Via: ViaCLI}, time.Now()); !errors.Is(err, ErrNotAMember) {
		t.Fatalf("removing a bay it is not in: %v", err)
	}
	added := queuedEvents(t, c, audit.BayMemberAdded)
	removed := queuedEvents(t, c, audit.BayMemberRemoved)
	if len(added) != 2 || len(removed) != 2 {
		t.Fatalf("audit added=%v removed=%v", added, removed)
	}
	if !strings.Contains(added[0], "sorting") || !strings.Contains(added[1], "left in no bay") {
		t.Fatalf("audit reasons %v", added)
	}
	if _, err := c.AddToBay(ctx, Membership{SessionUID: "nope", Bay: "work", Actor: "admin", Via: ViaCLI}, time.Now()); err == nil {
		t.Fatal("added a session that is not stored")
	}
}

func TestGrants(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	if _, err := c.CreateBay(ctx, "work", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddGrant(ctx, "user", "x", "work", PermRead, "admin", time.Now()); !errors.Is(err, ErrPrincipal) {
		t.Fatalf("bad kind: %v", err)
	}
	if _, err := c.AddGrant(ctx, PrincipalGroup, "eng", "work", "admin", "admin", time.Now()); !errors.Is(err, ErrPermission) {
		t.Fatalf("bad perm: %v", err)
	}
	if _, err := c.AddGrant(ctx, PrincipalGroup, "eng", "nope", PermRead, "admin", time.Now()); !errors.Is(err, ErrNoBay) {
		t.Fatalf("bad bay: %v", err)
	}
	if ok, err := c.AddGrant(ctx, PrincipalGroup, "eng", "work", PermRead, "admin", time.Now()); err != nil || !ok {
		t.Fatalf("grant ok=%v err=%v", ok, err)
	}
	if ok, _ := c.AddGrant(ctx, PrincipalGroup, "eng", "work", PermRead, "admin", time.Now()); ok {
		t.Fatal("second grant reported added")
	}
	if g, _ := c.Grants(ctx, PrincipalGroup, "eng"); len(g) != 1 || g[0].Permission != PermRead || g[0].GrantedBy != "admin" {
		t.Fatalf("grants %+v", g)
	}
	if ok, err := c.RemoveGrant(ctx, PrincipalGroup, "eng", "work", PermRead, "admin", time.Now()); err != nil || !ok {
		t.Fatalf("revoke ok=%v err=%v", ok, err)
	}
	if ok, _ := c.RemoveGrant(ctx, PrincipalGroup, "eng", "work", PermRead, "admin", time.Now()); ok {
		t.Fatal("second revoke reported removed")
	}
	if len(queuedEvents(t, c, audit.BayGrantAdded)) != 1 || len(queuedEvents(t, c, audit.BayGrantRemoved)) != 1 {
		t.Fatal("grant changes not audited once each")
	}
}

func TestPurgeRemovesMembership(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	p := &transcriptPoster{t: t, c: c, blobs: memBlobs{}, now: time.Now()}
	uid := p.mustPost("machine-a", "sessions/aaaa/sess-1.jsonl", []byte("{\"n\":1}\n")).SessionUID
	if ok, err := c.DeleteSession(ctx, uid); err != nil || !ok {
		t.Fatalf("purge ok=%v err=%v", ok, err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT count(*) FROM session_bays WHERE session_uid=?`, uid).Scan(&n); err != nil || n != 0 {
		t.Fatalf("membership rows after purge: %d err=%v", n, err)
	}
}

func TestDefaultOffRefusesANewSession(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	kept := newSession(t, c, "sess-before")
	if _, err := c.db.Exec(`UPDATE bays SET disabled=1 WHERE id=?`, DefaultBayID); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"n\":1}\n")
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessTerva, NativeSessionID: "sess-after",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/after.jsonl", Size: int64(len(body)), SHA256: digestHex(body)}}}
	if _, err := c.Ingest(ctx, m, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, memBlobs{digestHex(body): body}); !errors.Is(err, ErrNoBayForSession) {
		t.Fatalf("ingest with the default off: %v", err)
	}
	if _, ok, err := c.Alias(ctx, protocol.HarnessTerva, "sess-after", "machine-a"); err != nil || ok {
		t.Fatalf("refused session was stored: ok=%v err=%v", ok, err)
	}
	// A stored session still grows; the switch is about new sessions.
	first := []byte(`{"session":"sess-before"}` + "\n")
	grown := append(append([]byte{}, first...), `{"n":2}`+"\n"...)
	m.NativeSessionID = "sess-before"
	m.Artifacts = []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/sess-before.jsonl", Size: int64(len(grown)), SHA256: digestHex(grown)}}
	if _, err := c.Ingest(ctx, m, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, memBlobs{digestHex(grown): grown, digestHex(first): first}); err != nil {
		t.Fatalf("append with the default off: %v", err)
	}
	if got, _ := c.SessionBays(ctx, kept); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("kept session bays %v", got)
	}
}

// A stored session that loses its last bay goes to the inbox even with
// the default off: that switch is about new sessions at ingest.
func TestLastBayRemovedWithTheDefaultOffGoesToTheInbox(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	uid := newSession(t, c, "sess-sorted")
	if _, err := c.CreateBay(ctx, "work", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	m := Membership{SessionUID: uid, Bay: "work", Actor: "admin", Via: ViaCLI}
	if _, err := c.AddToBay(ctx, m, time.Now()); err != nil {
		t.Fatal(err)
	}
	m.Bay = DefaultBayName
	if err := c.RemoveFromBay(ctx, m, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(`UPDATE bays SET disabled=1 WHERE id=?`, DefaultBayID); err != nil {
		t.Fatal(err)
	}
	m.Bay = "work"
	if err := c.RemoveFromBay(ctx, m, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.SessionBays(ctx, uid); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("bays %v", got)
	}
}
