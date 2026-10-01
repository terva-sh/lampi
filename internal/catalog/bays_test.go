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
	a := newSession(t, c, "sess-a")
	b := newSession(t, c, "sess-b")
	devs, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, p.now)
	if err != nil {
		t.Fatal(err)
	}
	// Back to the schema before bays: a lake with sessions and a device.
	if _, err := c.db.Exec(`DROP TABLE bays; DROP TABLE bay_aliases; DROP TABLE session_bays; DROP TABLE session_bay_requests; DROP TABLE bay_grants; DROP TABLE bay_rules; DROP TABLE session_holds; ALTER TABLE registrations DROP COLUMN bays; ALTER TABLE read_tokens DROP COLUMN bay_scoped; PRAGMA user_version = 18`); err != nil {
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
	if m := c.Migrated(); m.From != 18 || len(m.Steps) != 3 || m.Steps[0] != "migrateBays" {
		t.Fatalf("migration: %+v", m)
	}
	for _, uid := range []string{a, b} {
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

func TestDeleteBayMovesOnlyOrphansToDefault(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	only := newSession(t, c, "sess-only")
	both := newSession(t, c, "sess-both")
	a, _ := c.CreateBay(ctx, "a", "admin", time.Now())
	b, _ := c.CreateBay(ctx, "b", "admin", time.Now())
	for _, m := range []Membership{{SessionUID: only, Bay: "a"}, {SessionUID: both, Bay: "a"}, {SessionUID: both, Bay: "b"}} {
		m.Actor, m.Via = "admin", ViaCLI
		if _, err := c.AddToBay(ctx, m, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for _, uid := range []string{only, both} {
		if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: DefaultBayName, Actor: "admin", Via: ViaCLI}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.AddGrant(ctx, PrincipalGroup, "eng", "a", PermRead, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Turned off, the default still takes a stored orphan.
	if err := c.SetDefaultEnabled(ctx, false, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	moved, err := c.DeleteBay(ctx, "a", "admin", time.Now())
	if err != nil || moved != 1 {
		t.Fatalf("moved=%d err=%v", moved, err)
	}
	if got, _ := c.SessionBays(ctx, only); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("orphan bays %v", got)
	}
	if got, _ := c.SessionBays(ctx, both); !reflect.DeepEqual(got, []string{b.ID}) {
		t.Fatalf("session still in b: %v", got)
	}
	if g, _ := c.Grants(ctx, PrincipalGroup, "eng"); len(g) != 0 {
		t.Fatalf("grants on a deleted bay: %+v", g)
	}
	if _, err := c.ResolveBay(ctx, a.ID); !errors.Is(err, ErrNoBay) {
		t.Fatalf("deleted bay resolves: %v", err)
	}
	if _, err := c.DeleteBay(ctx, DefaultBayName, "admin", time.Now()); !errors.Is(err, ErrDefaultOnly) {
		t.Fatalf("deleting default: %v", err)
	}
}

func TestSeedRoleGrantsRunsOnce(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	made, ran, err := c.SeedRoleGrants(ctx, []string{"viewers"}, []string{"ops"}, time.Now())
	if err != nil || !ran || len(made) != 3 {
		t.Fatalf("first seed made=%+v ran=%v err=%v", made, ran, err)
	}
	if got, _ := c.GroupBays(ctx, []string{"viewers", "ops"}, PermRead); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("read bays %v", got)
	}
	if got, _ := c.GroupBays(ctx, []string{"viewers"}, PermWrite); len(got) != 0 {
		t.Fatalf("viewer write bays %v", got)
	}
	if got, _ := c.GroupBays(ctx, []string{"ops"}, PermWrite); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("operator write bays %v", got)
	}
	// A group added to the web config later is not granted the inbox.
	made, ran, err = c.SeedRoleGrants(ctx, []string{"viewers", "late"}, nil, time.Now())
	if err != nil || ran || len(made) != 0 {
		t.Fatalf("second seed made=%+v ran=%v err=%v", made, ran, err)
	}
	if got, _ := c.GroupBays(ctx, []string{"late"}, PermRead); len(got) != 0 {
		t.Fatalf("late group bays %v", got)
	}
}

func TestRenameKeepsTheOldNameAsAnAlias(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	b, _ := c.CreateBay(ctx, "client-x", "admin", time.Now())
	if err := c.RenameBay(ctx, "client-x", "acme", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"acme", "client-x", b.ID} {
		if got, err := c.ResolveBay(ctx, ref); err != nil || got.ID != b.ID || got.Name != "acme" {
			t.Fatalf("resolve %s: %+v %v", ref, got, err)
		}
	}
	if _, err := c.CreateBay(ctx, "client-x", "admin", time.Now()); !errors.Is(err, ErrBayTaken) {
		t.Fatalf("new bay took an alias: %v", err)
	}
	if err := c.UnaliasBay(ctx, "client-x", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ResolveBay(ctx, "client-x"); !errors.Is(err, ErrNoBay) {
		t.Fatalf("removed alias resolves: %v", err)
	}
	if err := c.RenameBay(ctx, DefaultBayName, "inbox", "admin", time.Now()); err == nil {
		t.Fatal("renamed the default bay")
	}
}

func TestCodeBaysBecomeDeviceGrants(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	now := time.Now()
	work, _ := c.CreateBay(ctx, "work", "admin", now)
	gone, _ := c.CreateBay(ctx, "gone", "admin", now)
	if _, err := c.CreateRegistrationInBays(ctx, "box", strings.Repeat("1", 64), "", "k1", "cli", []string{"nope"}, Minter{Admin: true}, now, now.Add(time.Hour)); !errors.Is(err, ErrNoBay) {
		t.Fatalf("unknown bay: %v", err)
	}
	r, err := c.CreateRegistrationInBays(ctx, "box", strings.Repeat("1", 64), "", "k1", "cli", []string{"work", work.ID}, Minter{Admin: true}, now, now.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(r.Bays, []string{work.ID}) {
		t.Fatalf("registration %+v err=%v", r, err)
	}
	d, got, err := c.Redeem(ctx, strings.Repeat("1", 64), strings.Repeat("a", 64), "m1", nil, now, nil)
	if err != nil || !reflect.DeepEqual(got.Bays, []string{work.ID}) {
		t.Fatalf("redeem %+v err=%v", got, err)
	}
	if g, _ := c.Grants(ctx, PrincipalDevice, d.ID); len(g) != 1 || g[0].BayID != work.ID || g[0].Permission != PermWrite {
		t.Fatalf("device grants %+v", g)
	}
	// A code whose only bay was deleted before it was used writes the
	// inbox rather than nothing.
	if _, err := c.CreateRegistrationInBays(ctx, "box2", strings.Repeat("2", 64), "", "k1", "cli", []string{"gone"}, Minter{Admin: true}, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.DeleteBay(ctx, gone.ID, "admin", now); err != nil {
		t.Fatal(err)
	}
	d2, _, err := c.Redeem(ctx, strings.Repeat("2", 64), strings.Repeat("b", 64), "m2", nil, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := c.Grants(ctx, PrincipalDevice, d2.ID); len(g) != 1 || g[0].BayID != DefaultBayID {
		t.Fatalf("device from a code with a deleted bay: %+v", g)
	}
}

func TestBayScopedReadTokens(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	now := time.Now()
	inWork := newSession(t, c, "sess-work")
	inbox := newSession(t, c, "sess-inbox")
	work, _ := c.CreateBay(ctx, "work", "admin", now)
	if _, err := c.AddToBay(ctx, Membership{SessionUID: inWork, Bay: "work", Actor: "admin", Via: ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateReadToken(ctx, ReadToken{Label: "x", Permissions: []string{PermRawRead}, BayScoped: true, CreatedBy: "admin", Expires: now.Add(time.Hour)}, strings.Repeat("0", 64), now); err == nil {
		t.Fatal("a bay-scoped token with no bay was minted")
	}
	tok, err := c.CreateReadToken(ctx, ReadToken{Label: "ci", Permissions: []string{PermRawRead}, BayScoped: true, Bays: []string{"work"}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, strings.Repeat("1", 64), now)
	if err != nil || !reflect.DeepEqual(tok.Bays, []string{work.ID}) {
		t.Fatalf("token %+v err=%v", tok, err)
	}
	lake, err := c.CreateReadToken(ctx, ReadToken{Label: "all", Permissions: []string{PermRawRead}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, strings.Repeat("2", 64), now)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok, err := c.ReadTokenBySecret(ctx, strings.Repeat("1", 64))
	if err != nil || !ok || !stored.BayScoped {
		t.Fatalf("stored token %+v ok=%v err=%v", stored, ok, err)
	}
	reach := func(t2 ReadToken, uid string) bool {
		ok, err := c.ReadTokenReaches(ctx, t2, uid)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !reach(stored, inWork) || reach(stored, inbox) {
		t.Fatal("bay-scoped token reaches the wrong sessions")
	}
	if !reach(lake, inWork) || !reach(lake, inbox) {
		t.Fatal("a token from before bays lost its reach")
	}
	// Revoking its last bay leaves the token reading nothing, not the
	// whole lake.
	if _, err := c.RemoveGrant(ctx, PrincipalReadToken, tok.ID, "work", PermRead, "admin", now); err != nil {
		t.Fatal(err)
	}
	if reach(stored, inWork) || reach(stored, inbox) {
		t.Fatal("token with no bay left still reads")
	}
}

// TestAMintIsCheckedWhereTheCodeIsStored is review 1415: the minter's
// grants are read in the transaction that stores the code, so a grant
// revoked after the web's own check still refuses.
func TestAMintIsCheckedWhereTheCodeIsStored(t *testing.T) {
	ctx := context.Background()
	c, _ := openTemp(t)
	now := time.Now()
	if _, err := c.CreateBay(ctx, "work", "admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddGrant(ctx, PrincipalGroup, "ops", "work", PermWrite, "admin", now); err != nil {
		t.Fatal(err)
	}
	ops := Minter{Groups: []string{"ops"}}
	mint := func(name string, bays []string, m Minter) error {
		_, err := c.CreateRegistrationInBays(ctx, name, digestHex([]byte(name)), "", "k1", "web", bays, m, now, now.Add(time.Hour))
		return err
	}
	if err := mint("a", []string{"work"}, ops); err != nil {
		t.Fatal(err)
	}
	if err := mint("b", nil, ops); !errors.Is(err, ErrBayScope) {
		t.Fatalf("no bays without the default: %v", err)
	}
	if err := mint("c", []string{"work"}, Minter{}); !errors.Is(err, ErrBayScope) {
		t.Fatalf("zero minter: %v", err)
	}
	if _, err := c.RemoveGrant(ctx, PrincipalGroup, "ops", "work", PermWrite, "admin", now); err != nil {
		t.Fatal(err)
	}
	if err := mint("d", []string{"work"}, ops); !errors.Is(err, ErrBayScope) {
		t.Fatalf("after the revoke: %v", err)
	}
	if err := mint("e", []string{"work"}, Minter{Admin: true}); err != nil {
		t.Fatalf("admin: %v", err)
	}
}
