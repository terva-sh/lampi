package catalog

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// routed is a lake with bays work, secret and hold, and a device that
// may write work and the default.
type routed struct {
	t                   *testing.T
	c                   *Catalog
	dev                 Route
	work, secret, holdB Bay
}

func newRouted(t *testing.T) *routed {
	t.Helper()
	ctx := context.Background()
	c, _ := openTemp(t)
	if _, err := c.SyncTokenFile(ctx, []TokenEntry{{Hash: strings.Repeat("a", 64), Name: "laptop"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, err := c.DeviceByName(ctx, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	r := &routed{t: t, c: c, dev: Route{DeviceID: d.ID}}
	for name, b := range map[string]*Bay{"work": &r.work, "secret": &r.secret, "hold": &r.holdB} {
		if *b, err = c.CreateBay(ctx, name, "admin", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.AddGrant(ctx, PrincipalDevice, d.ID, "work", PermWrite, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	return r
}

// post ingests one transcript line for native, recorded in cwd, asking
// for bays.
func (r *routed) post(native, cwd string, bays ...string) (protocol.ManifestAck, error) {
	r.t.Helper()
	body := []byte(`{"session":"` + native + `"}` + "\n")
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Project:         protocol.Project{CWD: cwd},
		Artifacts: []protocol.Artifact{{
			Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/" + native + ".jsonl", Size: int64(len(body)), SHA256: digestHex(body),
		}},
		Bays: bays,
	}
	ack, _, err := r.c.IngestRouted(context.Background(), m, r.dev, time.Now(), []Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, memBlobs{digestHex(body): body})
	return ack, err
}

func (r *routed) mustPost(native, cwd string, bays ...string) protocol.ManifestAck {
	r.t.Helper()
	ack, err := r.post(native, cwd, bays...)
	if err != nil {
		r.t.Fatal(err)
	}
	return ack
}

func (r *routed) bays(uid string) []string {
	r.t.Helper()
	got, err := r.c.SessionBays(context.Background(), uid)
	if err != nil {
		r.t.Fatal(err)
	}
	slices.Sort(got)
	return got
}

func (r *routed) rule(action string, m config.ProjectMatch, bay string) BayRule {
	r.t.Helper()
	rule, err := r.c.AddBayRule(context.Background(), BayRule{Match: m, Action: action, BayID: bay}, "admin", time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return rule
}

func sorted(ids ...string) []string {
	slices.Sort(ids)
	return ids
}

func (r *routed) requests(uid string) map[string]string {
	r.t.Helper()
	rows, err := r.c.db.Query(`SELECT bay_ref, outcome, reason FROM session_bay_requests WHERE session_uid=?`, uid)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var ref, outcome, reason string
		if err := rows.Scan(&ref, &outcome, &reason); err != nil {
			r.t.Fatal(err)
		}
		out[ref] = strings.TrimSuffix(outcome+": "+reason, ": ")
	}
	return out
}

func TestRequestedBaysPlaceOnlyWhatTheDeviceMayWrite(t *testing.T) {
	r := newRouted(t)
	ack := r.mustPost("sess-1", "/src/a", "work", "secret", "nope", "work")
	if got := r.bays(ack.SessionUID); !reflect.DeepEqual(got, []string{r.work.ID}) {
		t.Fatalf("bays %v, want only work", got)
	}
	if !reflect.DeepEqual(ack.RefusedBays, []string{"secret", "nope"}) {
		t.Fatalf("refused %v", ack.RefusedBays)
	}
	want := map[string]string{"work": RequestAccepted, "secret": RequestRefused + ": " + RefusedNotGranted, "nope": RequestRefused + ": " + RefusedNoBay}
	if got := r.requests(ack.SessionUID); !reflect.DeepEqual(got, want) {
		t.Fatalf("requests %v", got)
	}
	if got := queuedEvents(t, r.c, audit.BayMemberAdded); len(got) != 1 || !strings.Contains(got[0], "via ingest: requested") {
		t.Fatalf("audit %v", got)
	}
}

func TestARefusedRequestLandsInDefault(t *testing.T) {
	r := newRouted(t)
	ack := r.mustPost("sess-1", "/src/a", "secret")
	if got := r.bays(ack.SessionUID); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("bays %v", got)
	}
	// An old agent asks for nothing and lands where it always did.
	old := r.mustPost("sess-2", "/src/a")
	if got := r.bays(old.SessionUID); !reflect.DeepEqual(got, []string{DefaultBayID}) || old.RefusedBays != nil {
		t.Fatalf("old agent: bays %v refused %v", got, old.RefusedBays)
	}
}

func TestRulesAddAndDenyOnEveryManifestAndOnlyAdd(t *testing.T) {
	r := newRouted(t)
	uid := r.mustPost("sess-1", "/src/a/app").SessionUID
	r.rule(RuleAdd, config.ProjectMatch{CWDPrefix: "/src/a"}, "secret")
	r.rule(RuleDeny, config.ProjectMatch{CWDPrefix: "/src/a"}, "work")
	// The stored session is routed again: secret is added, default stays.
	r.mustPost("sess-1", "/src/a/app", "work")
	if got := r.bays(uid); !reflect.DeepEqual(got, sorted(DefaultBayID, r.secret.ID)) {
		t.Fatalf("bays %v", got)
	}
	// A new session the rules place does not also land in default, and
	// deny wins over what the device asked for.
	fresh := r.mustPost("sess-2", "/src/a/lib", "work").SessionUID
	if got := r.bays(fresh); !reflect.DeepEqual(got, []string{r.secret.ID}) {
		t.Fatalf("new session bays %v", got)
	}
	// A rule on another folder does not reach this one.
	other := r.mustPost("sess-3", "/src/ab").SessionUID
	if got := r.bays(other); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("sibling folder bays %v", got)
	}
	if got := queuedEvents(t, r.c, audit.BayMemberAdded); len(got) != 2 || !strings.Contains(got[0], "via rule") {
		t.Fatalf("audit %v", got)
	}
}

func TestAHoldHoldsANewSessionAndFlagsAStoredOne(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	stored := r.mustPost("sess-old", "/src/client").SessionUID
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/client"}, "hold")
	r.rule(RuleAdd, config.ProjectMatch{CWDPrefix: "/src/client"}, "secret")

	fresh := r.mustPost("sess-new", "/src/client", "work").SessionUID
	if got := r.bays(fresh); !reflect.DeepEqual(got, []string{r.holdB.ID}) {
		t.Fatalf("held session bays %v", got)
	}
	if got := r.requests(fresh); got["work"] != RequestHeld {
		t.Fatalf("held request %v", got)
	}
	r.mustPost("sess-old", "/src/client", "work")
	if got := r.bays(stored); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("flagged session bays %v: a hold never removes", got)
	}
	holds, err := r.c.Holds(ctx)
	if err != nil || len(holds) != 2 {
		t.Fatalf("holds %+v err=%v", holds, err)
	}
	states := map[string]string{holds[0].SessionUID: holds[0].State, holds[1].SessionUID: holds[1].State}
	if states[fresh] != HoldHeld || states[stored] != HoldFlagged {
		t.Fatalf("hold states %v", states)
	}

	if err := r.c.ReleaseHold(ctx, fresh, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := r.bays(fresh); !reflect.DeepEqual(got, sorted(r.work.ID, r.secret.ID)) {
		t.Fatalf("released bays %v", got)
	}
	if got := r.requests(fresh); got["work"] != RequestAccepted {
		t.Fatalf("released request %v", got)
	}
	// Released once, the rule does not hold it again.
	r.mustPost("sess-new", "/src/client", "work")
	if got := r.bays(fresh); !reflect.DeepEqual(got, sorted(r.work.ID, r.secret.ID)) {
		t.Fatalf("after release bays %v", got)
	}
	if err := r.c.ReleaseHold(ctx, stored, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := r.bays(stored); !reflect.DeepEqual(got, sorted(DefaultBayID, r.work.ID, r.secret.ID)) {
		t.Fatalf("released flagged bays %v", got)
	}
	if err := r.c.ReleaseHold(ctx, stored, "admin", time.Now()); !errors.Is(err, ErrNoHold) {
		t.Fatalf("second release: %v", err)
	}
	if got := queuedEvents(t, r.c, audit.BayHoldReleased); len(got) != 2 {
		t.Fatalf("audit %v", got)
	}
}

func TestAReleasedHoldWithNothingElseGoesToDefault(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/client"}, "hold")
	uid := r.mustPost("sess-1", "/src/client").SessionUID
	if err := r.c.SetDefaultEnabled(ctx, false, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.c.ReleaseHold(ctx, uid, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("bays %v", got)
	}
}

func TestDefaultOffRefusesOnlyASessionNothingPlaces(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	if err := r.c.SetDefaultEnabled(ctx, false, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.post("sess-1", "/src/a", "secret"); !errors.Is(err, ErrNoBayForSession) {
		t.Fatalf("unplaced: %v", err)
	}
	ack := r.mustPost("sess-2", "/src/a", "work")
	if got := r.bays(ack.SessionUID); !reflect.DeepEqual(got, []string{r.work.ID}) {
		t.Fatalf("bays %v", got)
	}
	// The refused post left nothing behind.
	var n int
	if err := r.c.db.QueryRow(`SELECT count(*) FROM session_bay_requests WHERE bay_ref='secret'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("requests left by a refused post: %d err=%v", n, err)
	}
}

func TestBayRuleValidation(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	for _, tc := range []struct {
		rule BayRule
		want error
	}{
		{BayRule{Action: "move", Match: config.ProjectMatch{CWDPrefix: "/x"}, BayID: "work"}, ErrRuleAction},
		{BayRule{Action: RuleAdd, BayID: "work"}, ErrRuleEmpty},
		{BayRule{Action: RuleAdd, Match: config.ProjectMatch{CWDPrefix: "/x"}, BayID: "nope"}, ErrNoBay},
	} {
		if _, err := r.c.AddBayRule(ctx, tc.rule, "admin", time.Now()); !errors.Is(err, tc.want) {
			t.Errorf("%+v: err=%v want %v", tc.rule, err, tc.want)
		}
	}
	if _, err := r.c.AddBayRule(ctx, BayRule{Action: RuleAdd, Match: config.ProjectMatch{CWDGlob: "src/*"}, BayID: "work"}, "admin", time.Now()); err == nil {
		t.Error("a glob no rule may hold was accepted")
	}
	// A harness alone is a rule.
	byHarness, err := r.c.AddBayRule(ctx, BayRule{Action: RuleAdd, Harness: protocol.HarnessTerva, BayID: "secret"}, "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	uid := r.mustPost("sess-1", "/anywhere").SessionUID
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{r.secret.ID}) {
		t.Fatalf("harness rule bays %v", got)
	}
	if err := r.c.RemoveBayRule(ctx, byHarness.ID, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.c.RemoveBayRule(ctx, byHarness.ID, "admin", time.Now()); !errors.Is(err, ErrNoRule) {
		t.Fatalf("second remove: %v", err)
	}
	// Deleting a bay deletes its rules.
	r.rule(RuleAdd, config.ProjectMatch{CWDPrefix: "/src"}, "secret")
	if _, err := r.c.DeleteBay(ctx, "secret", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if rules, err := r.c.BayRules(ctx); err != nil || len(rules) != 0 {
		t.Fatalf("rules after delete %+v err=%v", rules, err)
	}
}

func TestTooManyRequestedBays(t *testing.T) {
	r := newRouted(t)
	many := make([]string, protocol.MaxManifestBays+1)
	for i := range many {
		many[i] = "b" + strings.Repeat("x", i)
	}
	if _, err := r.post("sess-1", "/src/a", many...); !errors.Is(err, ErrManyBays) {
		t.Fatalf("err=%v", err)
	}
}

// A request a deny rule matches is refused, recorded as denied, and
// named in the ack like any other refused bay (review 1432).
func TestADeniedRequestIsRefused(t *testing.T) {
	r := newRouted(t)
	r.rule(RuleDeny, config.ProjectMatch{CWDPrefix: "/src/a"}, "work")
	ack := r.mustPost("sess-1", "/src/a", "work")
	if !reflect.DeepEqual(ack.RefusedBays, []string{"work"}) {
		t.Fatalf("refused %v", ack.RefusedBays)
	}
	if got := r.requests(ack.SessionUID); got["work"] != RequestRefused+": "+RefusedDenied {
		t.Fatalf("requests %v", got)
	}
	if got := r.bays(ack.SessionUID); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("bays %v", got)
	}
}

// A held session that asked for its hold bay leaves it on release when
// a deny rule names that bay by then (review 1432).
func TestReleaseDropsADeniedHoldBay(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	if _, err := r.c.AddGrant(ctx, PrincipalDevice, r.dev.DeviceID, "hold", PermWrite, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/client"}, "hold")
	uid := r.mustPost("sess-1", "/src/client", "hold").SessionUID
	r.rule(RuleDeny, config.ProjectMatch{CWDPrefix: "/src/client"}, "hold")
	if err := r.c.ReleaseHold(ctx, uid, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("bays %v", got)
	}
	if got := r.requests(uid); got["hold"] != RequestRefused+": "+RefusedDenied {
		t.Fatalf("requests %v", got)
	}
}

// A bay that holds sessions is not deleted until they are released, so
// no held request is left with nothing to release it (review 1432).
func TestABayWithHoldsIsNotDeleted(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/client"}, "hold")
	uid := r.mustPost("sess-1", "/src/client", "work").SessionUID
	if _, err := r.c.DeleteBay(ctx, "hold", "admin", time.Now()); !errors.Is(err, ErrBayHolds) {
		t.Fatalf("delete with a hold: %v", err)
	}
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{r.holdB.ID}) {
		t.Fatalf("bays after refused delete %v", got)
	}
	if err := r.c.ReleaseHold(ctx, uid, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.c.DeleteBay(ctx, "hold", "admin", time.Now()); err != nil {
		t.Fatalf("delete after release: %v", err)
	}
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{r.work.ID}) {
		t.Fatalf("bays %v", got)
	}
}
