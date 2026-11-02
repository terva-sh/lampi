package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
)

func TestMigrationMultiBayApplyRetryAndRecoveryPreserveLaterWork(t *testing.T) {
	c, dir := openTemp(t)
	ctx, now := t.Context(), time.Now()
	one, two := newSession(t, c, "reviewer"), newSession(t, c, "rater")
	project, _ := c.CreateBay(ctx, "project", "admin", now)
	reviews, _ := c.CreateBay(ctx, "code-reviews", "admin", now)
	learning, _ := c.CreateBay(ctx, "learning", "admin", now)
	for _, uid := range []string{one, two} {
		if _, err := c.AddToBay(ctx, Membership{SessionUID: uid, Bay: project.ID, Actor: "admin", Via: ViaCLI}, now); err != nil {
			t.Fatal(err)
		}
	}
	req := BayMigrationRequest{UIDs: []string{two, one, one}, Add: []string{reviews.Name, learning.ID, reviews.ID}, Remove: []string{"default"}}
	plan, err := c.PlanBayMigration(ctx, AllBays(), req, now)
	if err != nil || plan.Changed != 2 || plan.Rejected != 0 || len(plan.Items) != 2 {
		t.Fatalf("plan %+v %v", plan, err)
	}
	// Preview writes no memberships, receipts or audit events.
	before, _ := c.SessionBays(ctx, one)
	if !slices.Contains(before, DefaultBayID) || slices.Contains(before, reviews.ID) {
		t.Fatal("preview wrote membership")
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), false, plan, false, "reader", ViaCLI, now); !errors.Is(err, ErrMigrationAuthority) {
		t.Fatalf("plan grants authority %v", err)
	}
	receipt, err := c.ApplyBayMigration(ctx, AllBays(), true, plan, false, "admin", ViaCLI, now)
	if err != nil || !receipt.Complete || len(receipt.Items) != 2 {
		t.Fatalf("apply %+v %v", receipt, err)
	}
	for _, uid := range []string{one, two} {
		bays, _ := c.SessionBays(ctx, uid)
		if len(bays) != 3 || !slices.Contains(bays, project.ID) || slices.Contains(bays, DefaultBayID) {
			t.Fatalf("multi preservation %v", bays)
		}
	}
	check, err := c.VerifyBayMigration(ctx, AllBays(), plan.ID)
	if err != nil || !check.Matches {
		t.Fatalf("verify %+v %v", check, err)
	}
	// A later unrelated membership must survive the inverse.
	later, _ := c.CreateBay(ctx, "later", "admin", now)
	if _, err := c.AddToBay(ctx, Membership{SessionUID: one, Bay: later.ID, Actor: "later", Via: ViaCLI}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	retry, err := c.ApplyBayMigration(ctx, AllBays(), true, plan, false, "admin", ViaCLI, now.Add(48*time.Hour))
	if err != nil || !retry.AlreadyApplied {
		t.Fatalf("retry %+v %v", retry, err)
	}
	inverse, err := c.RecoverBayMigration(ctx, AllBays(), plan.ID, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, inverse, false, "admin", ViaCLI, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	bays, _ := c.SessionBays(ctx, one)
	if len(bays) != 3 || !slices.Contains(bays, later.ID) || !slices.Contains(bays, DefaultBayID) || slices.Contains(bays, reviews.ID) {
		t.Fatalf("inverse lost later work %v", bays)
	}
	if err := c.FlushAudit(ctx, filepath.Dir(dir)); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "audit.jsonl"))
	if err != nil || !strings.Contains(string(log), "bay.migration.applied") || !strings.Contains(string(log), plan.ID) || !strings.Contains(string(log), `"actor":"admin"`) {
		t.Fatalf("audit missing migration/actor: %v", err)
	}
	var n int
	if err := c.db.QueryRow(`SELECT count(*) FROM bay_migrations`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("receipt count=%d %v", n, err)
	}
}

func TestMigrationStaleStateAndTamperedArtifacts(t *testing.T) {
	for _, kind := range []string{"membership", "hold", "grant", "rule", "rename", "routing", "destination-deleted", "tampered", "expired"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := openTemp(t)
			ctx, now := t.Context(), time.Now()
			uid := newSession(t, c, "reviewer")
			dst, _ := c.CreateBay(ctx, "reviews", "admin", now)
			plan, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Add: []string{dst.ID}, Remove: []string{"default"}}, now)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "membership":
				if _, err := c.AddToBay(ctx, Membership{SessionUID: uid, Bay: dst.ID, Actor: "other", Via: ViaCLI}, now); err != nil {
					t.Fatal(err)
				}
			case "hold":
				if _, err := c.db.Exec(`INSERT INTO session_holds(session_uid,bay_id,rule_id,state,created_at) VALUES(?,?,1,'flagged',?)`, uid, dst.ID, stamp(now)); err != nil {
					t.Fatal(err)
				}
			case "grant":
				if _, err := c.AddGrant(ctx, PrincipalGroup, "new-reader", dst.ID, PermRead, "admin", now); err != nil {
					t.Fatal(err)
				}
			case "rule":
				if _, err := c.AddBayRule(ctx, BayRule{Match: config.ProjectMatch{CWDPrefix: "/reviews"}, Action: RuleAdd, BayID: dst.ID}, "admin", now); err != nil {
					t.Fatal(err)
				}
			case "rename":
				if err := c.RenameBay(ctx, dst.ID, "renamed", "admin", now); err != nil {
					t.Fatal(err)
				}
			case "routing":
				if _, err := c.db.Exec(`UPDATE sessions SET manifest_json=json_set(manifest_json,'$.project.cwd','/different') WHERE session_uid=?`, uid); err != nil {
					t.Fatal(err)
				}
			case "destination-deleted":
				if _, err := c.DeleteBay(ctx, dst.ID, "admin", now); err != nil {
					t.Fatal(err)
				}
			case "tampered":
				plan.Items[0].Remove = nil
			case "expired":
				now = now.Add(25 * time.Hour)
			}
			if _, err := c.ApplyBayMigration(ctx, AllBays(), true, plan, false, "admin", ViaCLI, now); err == nil {
				t.Fatal("stale/tampered plan applied")
			}
			var n int
			if err := c.db.QueryRow(`SELECT count(*) FROM bay_migrations`).Scan(&n); err != nil || n != 0 {
				t.Fatal("failed apply has receipt")
			}
		})
	}
}

func TestMigrationProposedNoopFallbackAndRejectedItems(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	uid := newSession(t, c, "reviewer")
	req := BayMigrationRequest{UIDs: []string{uid}, Add: []string{"code-reviews", "learning"}, Proposed: []string{"code-reviews", "learning"}, Remove: []string{"default"}}
	plan, err := c.PlanBayMigration(ctx, AllBays(), req, now)
	if err != nil || len(plan.Destinations) != 2 {
		t.Fatalf("new bays %+v %v", plan, err)
	}
	if _, err := c.ResolveBay(ctx, "code-reviews"); !errors.Is(err, ErrNoBay) {
		t.Fatal("preview created bay")
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, plan, false, "admin", ViaCLI, now); err == nil {
		t.Fatal("implicit bay creation")
	}
	receipt, err := c.ApplyBayMigration(ctx, AllBays(), true, plan, true, "admin", ViaCLI, now)
	if err != nil || len(receipt.CreatedBays) != 2 {
		t.Fatalf("create %+v %v", receipt, err)
	}
	noop, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Add: []string{"code-reviews"}}, now)
	if err != nil || noop.Noop != 1 {
		t.Fatalf("noop %+v %v", noop, err)
	}
	scoped, err := c.PlanBayMigration(ctx, InBays([]string{receipt.CreatedBays[0].ID}), BayMigrationRequest{UIDs: []string{uid, "unknown"}, Add: []string{receipt.CreatedBays[0].ID}}, now)
	if err != nil || scoped.Rejected != 1 || scoped.CompleteMemberships {
		t.Fatalf("scoped %+v %v", scoped, err)
	}
	encoded, _ := json.Marshal(scoped)
	if strings.Contains(string(encoded), receipt.CreatedBays[1].Name) {
		t.Fatal("hidden co-membership exposed")
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, scoped, false, "admin", ViaCLI, now); !errors.Is(err, ErrMigrationAuthority) {
		t.Fatalf("scoped apply %v", err)
	}
	fallback, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Remove: []string{"code-reviews", "learning"}}, now)
	if err != nil || len(fallback.Items[0].After) != 1 || fallback.Items[0].After[0].ID != DefaultBayID {
		t.Fatalf("fallback %+v %v", fallback, err)
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, fallback, false, "admin", ViaCLI, now); err != nil {
		t.Fatal(err)
	}
	last, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Remove: []string{"default"}}, now)
	if err != nil || last.Rejected != 1 {
		t.Fatalf("last default %+v %v", last, err)
	}
	if _, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Add: []string{"default"}, Remove: []string{DefaultBayID}}, now); !errors.Is(err, ErrSameBay) {
		t.Fatalf("conflict alias %v", err)
	}
}

func TestMigrationRejectsHoldsAndRecoveryTouchedChanges(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	uid := newSession(t, c, "reviewer")
	dst, _ := c.CreateBay(ctx, "reviews", "admin", now)
	req := BayMigrationRequest{UIDs: []string{uid}, Add: []string{dst.ID}}
	p, err := c.PlanBayMigration(ctx, AllBays(), req, now)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := c.ApplyBayMigration(ctx, AllBays(), true, p, false, "admin", ViaCLI, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveFromBay(ctx, Membership{SessionUID: uid, Bay: dst.ID, Actor: "later", Via: ViaCLI}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddToBay(ctx, Membership{SessionUID: uid, Bay: dst.ID, Actor: "later", Via: ViaCLI}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RecoverBayMigration(ctx, AllBays(), receipt.ID, now.Add(3*time.Second)); !errors.Is(err, ErrMigrationStale) {
		t.Fatalf("recovery overwrites touched edits: %v", err)
	}
	if _, err := c.db.Exec(`INSERT INTO session_holds(session_uid,bay_id,rule_id,state,created_at) VALUES(?,?,1,'held',?)`, uid, dst.ID, stamp(now)); err != nil {
		t.Fatal(err)
	}
	p, err = c.PlanBayMigration(ctx, AllBays(), req, now)
	if err != nil || p.Rejected != 1 || p.Items[0].Status != "held_or_flagged" {
		t.Fatalf("held %+v %v", p, err)
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, p, false, "admin", ViaCLI, now); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("held apply %v", err)
	}
}

func TestSelectionFreezeAndHypotheticalRouting(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	uid := newSession(t, c, "reviewer")
	if _, err := c.db.Exec(`UPDATE sessions SET manifest_json=json_set(manifest_json,'$.project.cwd','/vuoro/run') WHERE session_uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	selection, err := c.SelectSessions(ctx, AllBays(), SessionSelectionRequest{Filter: &DiscoveryRequest{CWDPrefix: "/vuoro"}})
	if err != nil || !selection.Complete || len(selection.UIDs) != 1 {
		t.Fatalf("selection %+v %v", selection, err)
	}
	p, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: selection.UIDs, Add: []string{"default"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	later := newSession(t, c, "later")
	if _, err := c.db.Exec(`UPDATE sessions SET manifest_json=json_set(manifest_json,'$.project.cwd','/vuoro/run') WHERE session_uid=?`, later); err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 {
		t.Fatal("frozen cohort expanded")
	}
	preview, err := c.PreviewBayRule(ctx, AllBays(), BayRulePreviewRequest{Match: config.ProjectMatch{CWDPrefix: "/vuoro"}, Action: RuleAdd, Bay: "reviews", Proposed: true}, now)
	if err != nil || !preview.Complete || len(preview.UIDs) != 2 {
		t.Fatalf("routing catchment %+v %v", preview, err)
	}
	rules, _ := c.BayRules(ctx)
	if len(rules) != 0 {
		t.Fatal("preview installed routing")
	}
	selection, err = c.SelectSessions(ctx, AllBays(), SessionSelectionRequest{UIDs: []string{uid, uid, later}, Exclude: []string{later}})
	if err != nil || len(selection.UIDs) != 1 || len(selection.Excluded) != 1 {
		t.Fatal("exclude set")
	}
	if _, err := c.SelectSessions(ctx, AllBays(), SessionSelectionRequest{UIDs: make([]string, 501)}); !errors.Is(err, ErrSelectionLimit) {
		t.Fatal("selection silently sampled")
	}
}

func TestMigrationRejectedBatchIsAtomicAndReceiptsSurviveReopen(t *testing.T) {
	c, path := openTemp(t)
	ctx, now := t.Context(), time.Now()
	uid := newSession(t, c, "one")
	dst, err := c.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid, "unknown"}, Add: []string{dst.ID}}, now)
	if err != nil || p.Rejected != 1 || p.Changed != 1 {
		t.Fatalf("mixed plan %+v %v", p, err)
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, p, false, "admin", ViaCLI, now); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("mixed apply %v", err)
	}
	bays, _ := c.SessionBays(ctx, uid)
	if slices.Contains(bays, dst.ID) {
		t.Fatal("partial batch applied")
	}
	p, err = c.PlanBayMigration(ctx, AllBays(), BayMigrationRequest{UIDs: []string{uid}, Add: []string{dst.ID}}, now)
	if err != nil {
		t.Fatal(err)
	}
	// An append that leaves routing metadata alone is outside the plan state.
	if _, err := c.db.Exec(`UPDATE sessions SET head_sha256='later-head', manifest_json=json_set(manifest_json,'$.extra_append_hint',1) WHERE session_uid=?`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ApplyBayMigration(ctx, AllBays(), true, p, false, "admin", ViaCLI, now); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenCurrent(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retry, err := reopened.ApplyBayMigration(ctx, AllBays(), true, p, false, "admin", ViaCLI, now.Add(time.Hour))
	if err != nil || !retry.AlreadyApplied {
		t.Fatalf("durable retry %+v %v", retry, err)
	}
}

func TestMigrationScopedProposalsDoNotProbeHiddenNames(t *testing.T) {
	c, _ := openTemp(t)
	ctx, now := t.Context(), time.Now()
	uid := newSession(t, c, "one")
	secret, err := c.CreateBay(ctx, "private-client", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddToBay(ctx, Membership{SessionUID: uid, Bay: secret.ID, Actor: "admin", Via: ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"private-client", "unused-name"} {
		p, err := c.PlanBayMigration(ctx, InBays([]string{DefaultBayID}), BayMigrationRequest{UIDs: []string{uid}, Add: []string{name}, Proposed: []string{name}}, now)
		if err != nil || len(p.Destinations) != 1 || !p.Destinations[0].Proposed || p.CompleteMemberships {
			t.Fatalf("name probe %s %+v %v", name, p, err)
		}
		raw, _ := json.Marshal(p.Items)
		if strings.Contains(string(raw), secret.ID) {
			t.Fatal("hidden membership disclosed")
		}
	}
}
