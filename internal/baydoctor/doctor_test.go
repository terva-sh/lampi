package baydoctor

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

func fixturePolicy() Policy {
	return Policy{Mappings: []Mapping{{ID: "review-layout", Match: config.ProjectMatch{CWDPrefix: "/linux/vuoro/worktrees"}, Harness: "claude", Destinations: []string{"reviews", "learning"}, Reason: "Operator-verified review worktree layout; inspect each run"}, {ID: "mac-review-layout", Match: config.ProjectMatch{CWDPrefix: "/mac/reviews"}, Harness: "claude", Destinations: []string{"reviews"}, Reason: "Separately verified macOS layout"}}}
}
func metadata(uid, cwd, harness string) catalog.SessionMetadata {
	return catalog.SessionMetadata{SessionSummary: catalog.SessionSummary{UID: uid, Harness: harness, ProjectID: "same-repo"}, CWD: cwd, Bays: []catalog.BayReference{{ID: catalog.DefaultBayID, Name: "default"}}}
}

// These labels describe the purpose established by fixture authors. The
// engine deliberately does not inspect text; reused directories remain a
// measured false positive and therefore every suggestion stays uncertain.
func TestLabeledHeuristicMatrix(t *testing.T) {
	policy := fixturePolicy()
	resolved := map[string]string{"reviews": "review-id", "learning": "learning-id"}
	now := time.Now()
	cases := []struct {
		name, cwd, harness string
		review, suggest    bool
	}{
		{"reviewer", "/linux/vuoro/worktrees/run-1", "claude", true, true},
		{"unlinked-rater", "/linux/vuoro/worktrees/run-1", "claude", true, true},
		{"development-same-repo", "/repo/project", "claude", false, false},
		{"development-discussing-review", "/repo/project", "claude", false, false},
		{"sdk-other-job", "/sdk/job", "claude", false, false},
		{"reused-review-directory", "/linux/vuoro/worktrees/reused", "claude", false, true},
		{"mac-reviewer", "/mac/reviews/run-2", "claude", true, true},
		{"unknown-device-layout", "/other/review-run", "claude", true, false},
		{"missing-directory", "", "claude", true, false},
		{"wrong-harness", "/linux/vuoro/worktrees/run-1", "codex", false, false},
		{"path-lookalike", "/linux/vuoro/worktrees-old/run", "claude", false, false},
	}
	fp, fn, tp, tn := 0, 0, 0, 0
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := metadata(tc.name, tc.cwd, tc.harness)
			if tc.name == "unlinked-rater" {
				s.ProjectID = ""
			}
			r := Evaluate([]catalog.SessionMetadata{s}, policy, resolved, "scope", true, now)
			suggest := len(r.Findings) > 0
			if suggest != tc.suggest {
				t.Fatalf("%+v", r)
			}
			if suggest {
				if r.Findings[0].Uncertainty == "" || r.Findings[0].Preview == nil {
					t.Fatal("missing uncertainty or handoff")
				}
				if tc.review {
					tp++
				} else {
					fp++
				}
			} else if tc.review {
				fn++
			} else {
				tn++
			}
		})
	}
	if tp != 3 || fp != 1 || fn != 2 || tn != 5 {
		t.Fatalf("matrix TP=%d FP=%d FN=%d TN=%d", tp, fp, fn, tn)
	}
	t.Logf("metadata mapping matrix TP=%d FP=%d FN=%d TN=%d; unsuitable for autonomous classification", tp, fp, fn, tn)
}

func TestAlternativesPlacementMissingEvidenceAndIgnore(t *testing.T) {
	p := fixturePolicy()
	s := metadata("review", "/linux/vuoro/worktrees/run", "claude")
	now := time.Now()
	resolved := map[string]string{"reviews": "review-id", "learning": "learning-id"}
	r := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, now)
	if len(r.Findings) != 1 || len(r.Findings[0].Destinations) != 2 {
		t.Fatal("compatible destinations", r)
	}
	f := r.Findings[0]
	p.Ignore = []Ignore{{FindingID: f.ID, Scope: "scope", Version: Version, Until: now.Add(time.Hour)}}
	if r := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, now); r.Suppressed != 1 || len(r.Findings) != 0 {
		t.Fatal("ignore", r)
	}
	for _, scope := range []string{"scope", "other-scope"} {
		r := Evaluate([]catalog.SessionMetadata{s}, p, resolved, scope, true, now.Add(2*time.Hour))
		if len(r.Findings) != 1 {
			t.Fatal("expired ignore")
		}
	}
	if len(Evaluate([]catalog.SessionMetadata{s}, p, resolved, "other-scope", true, now).Findings) != 1 {
		t.Fatal("ignore crossed scope")
	}
	p.Ignore = nil
	s.Bays = append(s.Bays, catalog.BayReference{ID: "review-id"}, catalog.BayReference{ID: "learning-id"})
	if r := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, now); r.AlreadyPlaced != 1 || len(r.Findings) != 0 {
		t.Fatal("duplicate work", r)
	}
	s.Bays = s.Bays[:1]
	r = Evaluate([]catalog.SessionMetadata{s}, p, map[string]string{}, "scope", true, now)
	if r.Findings[0].Status != "unknown_or_inaccessible_destination" || r.Findings[0].Preview != nil {
		t.Fatal("missing destination", r)
	}
	r = Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", false, now)
	if r.Complete || r.Findings[0].Preview != nil || r.Findings[0].Status != "incomplete_inventory" {
		t.Fatal("incomplete", r)
	}
	s.MetadataTruncated = true
	if r := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, now); len(r.Findings) != 0 || r.Abstentions[0].Code != "incomplete_metadata" {
		t.Fatal("clipped", r)
	}
	s.MetadataTruncated = false
	p.Mappings[0].Alternative = "purpose"
	other := p.Mappings[0]
	other.ID = "conflict"
	other.Destinations = []string{"development"}
	p.Mappings = append(p.Mappings, other)
	r = Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, now)
	if len(r.Findings) != 0 || r.Abstentions[0].Code != "conflicting_alternatives" {
		t.Fatal("alternatives", r)
	}
}
func TestPolicyValidationAndStableIDs(t *testing.T) {
	p := fixturePolicy()
	if Validate(p) != nil {
		t.Fatal("valid policy refused")
	}
	for _, raw := range []string{`{"unknown":true}`, `{"mappings":[]} trailing`, `{"mappings":[{"id":"broad","match":{"git_remote":"repo"},"destinations":["reviews"],"reason":"repository only"}]}`} {
		if _, err := DecodePolicy([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	s := metadata("review", "/linux/vuoro/worktrees/run", "claude")
	resolved := map[string]string{"reviews": "review-id", "learning": "learning-id"}
	a := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, time.Now())
	slices.Reverse(p.Mappings[0].Destinations)
	b := Evaluate([]catalog.SessionMetadata{s}, p, resolved, "scope", true, time.Now())
	if a.Findings[0].ID != b.Findings[0].ID {
		t.Fatal("unstable finding IDs")
	}
}

func TestOverlappingMappingsRespectMigrationDestinationLimit(t *testing.T) {
	p := fixturePolicy()
	p.Mappings = p.Mappings[:1]
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("category-%d", i)
		p.Mappings[0].Destinations = append(p.Mappings[0].Destinations, name)
		p.Mappings[0].Proposed = append(p.Mappings[0].Proposed, name)
	}
	// Each mapping is within its own limit, but their compatible union may be
	// larger than an atomic migration accepts.
	other := p.Mappings[0]
	other.ID = "more"
	other.Destinations = other.Destinations[10:]
	other.Proposed = other.Proposed[8:]
	p.Mappings[0].Destinations = p.Mappings[0].Destinations[:12]
	p.Mappings[0].Proposed = p.Mappings[0].Proposed[:10]
	p.Mappings = append(p.Mappings, other)
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
	r := Evaluate([]catalog.SessionMetadata{metadata("review", "/linux/vuoro/worktrees/run", "claude")}, p, map[string]string{"reviews": "reviews", "learning": "learning"}, "scope", true, time.Now())
	if len(r.Findings) != 1 || r.Findings[0].Status != "destination_limit" || r.Findings[0].Preview != nil {
		t.Fatal("unusable handoff", r)
	}
}
