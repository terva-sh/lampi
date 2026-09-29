package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/config"
)

func inboxReasons(t *testing.T, c *Catalog) map[string][]string {
	t.Helper()
	entries, err := c.Inbox(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, e := range entries {
		if len(e.Reasons) == 0 {
			t.Errorf("%s listed with no reason", e.SessionUID)
		}
		out[e.SessionUID] = e.Reasons
	}
	return out
}

func TestInboxGivesEveryEntryAReason(t *testing.T) {
	r := newRouted(t)
	plain := r.mustPost("sess-plain", "/src/a").SessionUID
	refused := r.mustPost("sess-refused", "/src/b", "secret").SessionUID
	placedButRefused := r.mustPost("sess-mixed", "/src/c", "work", "nope").SessionUID
	sorted := r.mustPost("sess-sorted", "/src/d", "work").SessionUID
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/held"}, "hold")
	held := r.mustPost("sess-held", "/src/held").SessionUID

	got := inboxReasons(t, r.c)
	want := map[string]string{
		plain:            ReasonNothingPlaced,
		refused:          "asked for bay secret: refused, not granted",
		placedButRefused: "asked for bay nope: refused, no such bay",
		held:             "held by hold rule 1 into bay hold",
	}
	for uid, reason := range want {
		if !reflect.DeepEqual(got[uid], []string{reason}) {
			t.Errorf("%s: reasons %v want %q", uid, got[uid], reason)
		}
	}
	if _, listed := got[sorted]; listed {
		t.Errorf("a session placed as asked is in the inbox: %v", got[sorted])
	}
}

func TestMoveSessionsByFilterWithDryRun(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	a := r.mustPost("sess-a", "/src/client/app").SessionUID
	b := r.mustPost("sess-b", "/src/client/lib").SessionUID
	other := r.mustPost("sess-c", "/src/other").SessionUID
	if _, err := r.c.MoveSessions(ctx, Move{From: DefaultBayName, To: "work", Actor: "admin"}, time.Now()); !errors.Is(err, ErrNoFilter) {
		t.Fatalf("no filter: %v", err)
	}
	move := Move{From: DefaultBayName, To: "work", Filter: SessionFilter{CWDPrefix: "/src/client"}, Actor: "admin", DryRun: true}
	uids, err := r.c.MoveSessions(ctx, move, time.Now())
	if err != nil || !reflect.DeepEqual(uids, []string{a, b}) {
		t.Fatalf("dry run %v err=%v", uids, err)
	}
	if got := r.bays(a); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("a dry run moved: %v", got)
	}
	if got := queuedEvents(t, r.c, audit.BayMemberAdded); len(got) != 0 {
		t.Fatalf("a dry run audited %v", got)
	}
	move.DryRun = false
	if uids, err = r.c.MoveSessions(ctx, move, time.Now()); err != nil || len(uids) != 2 {
		t.Fatalf("move %v err=%v", uids, err)
	}
	for _, uid := range []string{a, b} {
		if got := r.bays(uid); !reflect.DeepEqual(got, []string{r.work.ID}) {
			t.Errorf("%s bays %v", uid, got)
		}
	}
	if got := r.bays(other); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Errorf("unmatched session moved: %v", got)
	}
	if got := queuedEvents(t, r.c, audit.BayMemberRemoved); len(got) != 2 || !strings.Contains(got[0], "bulk move to") {
		t.Errorf("audit %v", got)
	}
	// Filters combine, and a harness or device that matches nothing
	// moves nothing.
	for _, f := range []SessionFilter{{Harness: "codex"}, {CWDPrefix: "/src/other", Harness: "claude"}} {
		if uids, err := r.c.MoveSessions(ctx, Move{From: DefaultBayName, To: "work", Filter: f, Actor: "admin"}, time.Now()); err != nil || len(uids) != 0 {
			t.Errorf("%+v moved %v err=%v", f, uids, err)
		}
	}
	d, err := r.c.DeviceByID(ctx, r.dev.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if uids, err := r.c.MoveSessions(ctx, Move{From: DefaultBayName, To: "secret", Filter: SessionFilter{Device: d.ID}, Actor: "admin", DryRun: true}, time.Now()); err != nil || len(uids) != 0 {
		// The routed test device never bound a machine, so it has no sessions.
		t.Errorf("device filter %v err=%v", uids, err)
	}
}

func TestApplyRulesIsAddOnlyWithDryRun(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	a := r.mustPost("sess-a", "/src/client/app").SessionUID
	flag := r.mustPost("sess-f", "/src/held").SessionUID
	add := r.rule(RuleAdd, config.ProjectMatch{CWDPrefix: "/src/client"}, "secret")
	r.rule(RuleHold, config.ProjectMatch{CWDPrefix: "/src/held"}, "hold")
	dry, err := r.c.ApplyRules(ctx, "admin", true, time.Now())
	if err != nil || len(dry) != 2 {
		t.Fatalf("dry run %+v err=%v", dry, err)
	}
	if got := r.bays(a); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("a dry run wrote %v", got)
	}
	done, err := r.c.ApplyRules(ctx, "admin", false, time.Now())
	if err != nil || !reflect.DeepEqual(done, dry) {
		t.Fatalf("applied %+v want %+v err=%v", done, dry, err)
	}
	want := []Applied{{SessionUID: a, BayID: r.secret.ID, RuleID: add.ID, Action: RuleAdd}}
	if !reflect.DeepEqual(done[:1], want) || done[1].SessionUID != flag || done[1].Action != RuleHold {
		t.Fatalf("applied %+v", done)
	}
	if got := r.bays(a); !reflect.DeepEqual(got, sorted(DefaultBayID, r.secret.ID)) {
		t.Fatalf("add-only kept the default: %v", got)
	}
	if got := r.bays(flag); !reflect.DeepEqual(got, []string{DefaultBayID}) {
		t.Fatalf("a flagged session changed bays: %v", got)
	}
	if again, err := r.c.ApplyRules(ctx, "admin", false, time.Now()); err != nil || len(again) != 0 {
		t.Fatalf("a second run changed %+v err=%v", again, err)
	}
}

func TestBayProblems(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	uid := r.mustPost("sess-a", "/src/a").SessionUID
	if p, err := r.c.BayProblems(ctx); err != nil || len(p) != 0 {
		t.Fatalf("clean lake: %+v err=%v", p, err)
	}
	if _, err := r.c.db.Exec(`DELETE FROM session_bays WHERE session_uid=?; INSERT INTO bay_grants VALUES ('group','g','bay_gone','read','','t')`, uid); err != nil {
		t.Fatal(err)
	}
	p, err := r.c.BayProblems(ctx)
	if err != nil || !reflect.DeepEqual(p, []BayProblem{{"sessions in no bay", 1}, {"grants naming a bay that is gone", 1}}) {
		t.Fatalf("problems %+v err=%v", p, err)
	}
	if got := inboxReasons(t, r.c); !strings.Contains(strings.Join(got[uid], ""), "in no bay") {
		t.Fatalf("inbox %v", got)
	}
}

// A session in no bay is placed by a move from the default bay, as the
// inbox says (review 1461).
func TestMoveFromDefaultPlacesASessionInNoBay(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	uid := r.mustPost("sess-a", "/src/a").SessionUID
	if _, err := r.c.db.Exec(`DELETE FROM session_bays WHERE session_uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	if moved, err := r.c.MoveSessions(ctx, Move{From: "work", To: "secret", Filter: SessionFilter{CWDPrefix: "/src/a"}, Actor: "admin"}, time.Now()); err != nil || len(moved) != 0 {
		t.Fatalf("from another bay: %v %v", moved, err)
	}
	moved, err := r.c.MoveSessions(ctx, Move{From: DefaultBayName, To: "work", Filter: SessionFilter{CWDPrefix: "/src/a"}, Actor: "admin"}, time.Now())
	if err != nil || !reflect.DeepEqual(moved, []string{uid}) {
		t.Fatalf("moved %v err=%v", moved, err)
	}
	if got := r.bays(uid); !reflect.DeepEqual(got, []string{r.work.ID}) {
		t.Fatalf("bays %v", got)
	}
}

// A session a rule added to another bay, and that stayed in the default,
// is not described as one nothing placed (review 1461).
func TestInboxNamesASessionAlsoInDefault(t *testing.T) {
	ctx := context.Background()
	r := newRouted(t)
	uid := r.mustPost("sess-a", "/src/a").SessionUID
	r.rule(RuleAdd, config.ProjectMatch{CWDPrefix: "/src/a"}, "secret")
	if _, err := r.c.ApplyRules(ctx, "admin", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := inboxReasons(t, r.c)[uid]; !reflect.DeepEqual(got, []string{ReasonAlsoInDefault}) {
		t.Fatalf("reasons %v", got)
	}
	if _, err := r.c.MoveSessions(ctx, Move{From: DefaultBayName, To: "secret", Filter: SessionFilter{CWDPrefix: "/src/a"}, Actor: "admin"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, listed := inboxReasons(t, r.c)[uid]; listed {
		t.Fatal("still in the inbox after the move its reason names")
	}
}
