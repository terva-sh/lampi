package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// Execute the documented agent/host workflow against Vuoro-shaped metadata,
// without depending on a production transcript, credential or machine path.
func TestOrganizationWalkthroughCompanionsExclusionsAndLaterManifests(t *testing.T) {
	lake, srv, _ := mcpLake(t)
	ctx := t.Context()
	now := time.Now()
	data := filepath.Dir(lake.Normalized)
	token := tokenFile(t, lake, catalog.PermEventsRead)
	manifests := map[string]protocol.Manifest{}
	seed := func(native, cwd, project string) string {
		m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "synthetic-machine", Harness: "claude", NativeSessionID: native, Project: protocol.Project{CWD: cwd, CWDHash: "hash-" + cwd, ProjectID: project}, Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "session.jsonl", SHA256: strings.Repeat("a", 64), Size: 12}}}
		a, err := lake.Catalog.Ingest(ctx, m, now, []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		manifests[a.SessionUID] = m
		return a.SessionUID
	}
	reviewer := seed("reviewer", "/verified/review/runs/one", "repo-a")
	rater := seed("finding-rater", "/verified/review/runs/one", "")
	development := seed("development", "/repo/project", "repo-a")
	project, _ := lake.Catalog.CreateBay(ctx, "project-a", "test", now)
	_, _ = lake.Catalog.CreateBay(ctx, "learning", "test", now)
	for _, uid := range []string{reviewer, rater} {
		if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: project.ID, Actor: "test", Via: catalog.ViaCLI}, now); err != nil {
			t.Fatal(err)
		}
	}
	var out, stderr bytes.Buffer
	env := Env{Stdout: &out, Stderr: &stderr}
	run := func(args ...string) {
		t.Helper()
		out.Reset()
		if err := Run(args, env); err != nil {
			t.Fatal(args, err, stderr.String())
		}
	}
	remote := func(args ...string) {
		t.Helper()
		run(append(append([]string{"query"}, args...), "--server", srv.URL, "--token-file", token)...)
	}
	host := func(args ...string) {
		t.Helper()
		run(append(append([]string{"serve", "bays"}, args...), "--data", data)...)
	}
	remote("sessions", "--harness", "claude", "--cwd-prefix", "/verified/review/runs")
	var page catalog.Page[catalog.SessionMetadata]
	if json.Unmarshal(out.Bytes(), &page) != nil || len(page.Items) != 2 || page.NextCursor != "" {
		t.Fatal("complete companion inventory", out.String())
	}
	if !slices.ContainsFunc(page.Items, func(s catalog.SessionMetadata) bool { return s.UID == rater && s.ProjectID == "" }) {
		t.Fatal("unlinked companion omitted")
	}
	remote("sessions", "--cwd-prefix", "/absent")
	if !strings.Contains(out.String(), `"items":[]`) {
		t.Fatal("empty listing", out.String())
	}
	policy := baydoctor.Policy{Mappings: []baydoctor.Mapping{{ID: "verified-review-layout", Match: config.ProjectMatch{CWDPrefix: "/verified/review/runs"}, Harness: "claude", Destinations: []string{"code-reviews", "learning"}, Proposed: []string{"code-reviews"}, Reason: "Inspected synthetic reviewer/rater and development controls"}}}
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "policy.json")
	raw, _ := json.Marshal(policy)
	if os.WriteFile(policyFile, raw, 0600) != nil {
		t.Fatal("policy")
	}
	remote("recommend-bays", "--policy", policyFile, "--json", "--harness", "claude")
	var report baydoctor.Report
	if json.Unmarshal(out.Bytes(), &report) != nil || len(report.Findings) != 2 || report.Unmatched != 1 {
		t.Fatal("doctor cohort", out.String())
	}
	// An exact reviewer cohort can target the existing empty learning bay;
	// neither that cohort nor --bays default restricts destination authority.
	for _, doctor := range []func(...string){
		func(args ...string) { host(append([]string{"doctor"}, args...)...) },
		func(args ...string) { remote(append([]string{"recommend-bays"}, args...)...) },
	} {
		doctor("--policy", policyFile, "--json", "--session", reviewer, "--bays", "default")
		var exact baydoctor.Report
		if json.Unmarshal(out.Bytes(), &exact) != nil || exact.Inspected != 1 || len(exact.Findings) != 1 || exact.Findings[0].UID != reviewer || exact.Findings[0].Preview == nil || len(exact.Findings[0].Preview.Add) != 2 {
			t.Fatal("exact doctor destination authority", out.String())
		}
	}
	selectionFile := filepath.Join(dir, "selection.json")
	host("select", "--session", reviewer, "--session", rater, "--session", development, "--session", reviewer, "--exclude", development, "--out", selectionFile)
	selectedRaw, _ := os.ReadFile(selectionFile)
	var selected catalog.SessionSelection
	_ = json.Unmarshal(selectedRaw, &selected)
	if len(selected.UIDs) != 2 || !selected.Complete || len(selected.Excluded) != 1 {
		t.Fatal("exact exceptions", string(selectedRaw))
	}
	addPlan := filepath.Join(dir, "add.json")
	host("plan", "--sessions-file", selectionFile, "--add", "code-reviews,learning", "--propose", "code-reviews", "--out", addPlan)
	raw, _ = os.ReadFile(addPlan)
	var add catalog.BayMigrationPlan
	_ = json.Unmarshal(raw, &add)
	if len(add.Items) != 2 || len(add.Items[0].After) != 4 {
		t.Fatal("add retains memberships", string(raw))
	}
	movePlan := filepath.Join(dir, "move.json")
	host("plan", "--sessions-file", selectionFile, "--add", "code-reviews,learning", "--from", "default", "--propose", "code-reviews", "--out", movePlan)
	raw, _ = os.ReadFile(movePlan)
	var move catalog.BayMigrationPlan
	_ = json.Unmarshal(raw, &move)
	if move.Rejected != 0 || move.Changed != 2 || len(move.Items[0].After) != 3 {
		t.Fatal("move preview", string(raw))
	}
	host("apply-plan", movePlan, "--yes", "--create-proposed")
	host("verify-plan", move.ID)
	if !strings.Contains(out.String(), `"matches_receipt":true`) {
		t.Fatal("verification", out.String())
	}
	// Reingesting the same manifests does not itself restore default. A later
	// rule explicitly matching that source does restore it, without dropping
	// unrelated named bays. This is why future routing is a separate review.
	for _, uid := range []string{reviewer, rater} {
		_, err := lake.Catalog.Ingest(ctx, manifests[uid], now.Add(time.Minute), []catalog.Decision{{Relation: protocol.RelationUnchanged}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		bays, _ := lake.Catalog.SessionBays(ctx, uid)
		if slices.Contains(bays, catalog.DefaultBayID) || len(bays) != 3 {
			t.Fatal("unexpected source restoration", bays)
		}
	}
	host("preview-rule", "--to", "default", "--rule-action", "add", "--cwd-prefix", "/verified/review/runs", "--harness", "claude")
	var catchment catalog.BayRulePreview
	_ = json.Unmarshal(out.Bytes(), &catchment)
	if len(catchment.UIDs) != 2 || !catchment.Complete {
		t.Fatal("separate catchment", out.String())
	}
	if _, err := lake.Catalog.AddBayRule(ctx, catalog.BayRule{Match: config.ProjectMatch{CWDPrefix: "/verified/review/runs"}, Harness: "claude", Action: catalog.RuleAdd, BayID: catalog.DefaultBayID}, "test", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err := lake.Catalog.Ingest(ctx, manifests[reviewer], now.Add(3*time.Minute), []catalog.Decision{{Relation: protocol.RelationUnchanged}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bays, _ := lake.Catalog.SessionBays(ctx, reviewer)
	if !slices.Contains(bays, catalog.DefaultBayID) || len(bays) != 4 {
		t.Fatal("later rule behavior", bays)
	}
	devBays, _ := lake.Catalog.SessionBays(ctx, development)
	if len(devBays) != 1 || devBays[0] != catalog.DefaultBayID {
		t.Fatal("development moved", devBays)
	}
}
