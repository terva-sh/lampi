package profiledoctor

import (
	"fmt"
	"slices"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func cwd(s string) config.ProjectMatch    { return config.ProjectMatch{CWDPrefix: s} }
func remote(s string) config.ProjectMatch { return config.ProjectMatch{GitRemote: s} }

func project(remote, cwd string, allowed bool) protocol.InventoryProject {
	p := protocol.InventoryProject{GitRemote: remote, CWD: cwd, CWDs: 1, Sessions: 2, Allowed: allowed}
	if !allowed {
		p.Reason = config.RefusedNoMatch
	}
	return p
}

// find is the finding with code for the allow rule at index, or nil.
func find(rep Report, code string, index int) *Finding {
	for i, f := range rep.Findings {
		if f.Code == code && f.Index == index {
			return &rep.Findings[i]
		}
	}
	return nil
}

func TestDiagnose(t *testing.T) {
	allow := []config.ProjectMatch{
		/* 0 */ remote("git.example/team/app"),
		/* 1 */ cwd("/work/app"),
		/* 2 */ cwd("/work"),
		/* 3 */ cwd("/home/steamos"),
		/* 4 */ {GitRemotePrefix: "github.com"},
		/* 5 */ cwd("/tmp/t3code-title-a"),
		/* 6 */ cwd("/home/me/.cache/scratch/gone"),
		/* 7 */ remote("git.example/team/new"),
		/* 8 */ cwd("/srv/secret/x"),
	}
	// A deny git_remote_prefix would also refuse every folder project,
	// whose remote is unknown, so the fixture denies by folder.
	deny := []config.ProjectMatch{cwd("/srv/secret")}
	steam := project("", "/home/steamos", true)
	steam.CWDHash = "0123456789abcdef"
	in := Input{
		Projects: config.Projects{Allow: allow, Deny: deny},
		Devices: []Device{{Name: "deck", Listed: true, Projects: []protocol.InventoryProject{
			project("git.example/team/app", "/work/app", true),
			steam,
			project("", "/home/steamos/games/x", true),
			project("github.com/someone/thing", "/src/thing", true),
			project("", "/tmp/t3code-title-a", true),
			project("", "/home/me/.vuoro/wt/a", false),
			project("", "/home/me/.vuoro/wt/b", false),
			project("", "/home/me/.vuoro/wt/c", false),
		}}},
		Hidden: map[catalog.ProjectKey]bool{},
	}
	rep := Diagnose(in)

	if f := find(rep, CodeCovered, 1); f == nil || f.Fix != FixSafe || *f.By != cwd("/work") {
		t.Errorf("covered: %+v", f)
	}
	if f := find(rep, CodeShadowed, 8); f == nil || f.Fix != FixSafe {
		t.Errorf("shadowed: %+v", f)
	}
	f := find(rep, CodeHomeFolder, 3)
	if f == nil || f.Level != LevelProblem || f.Fix != FixNarrow {
		t.Fatalf("home folder: %+v", f)
	}
	// The session started in the home directory keeps a cwd_hash, which
	// names that folder alone; the one under it keeps its folder.
	if want := []config.ProjectMatch{{CWDHash: "0123456789abcdef"}, cwd("/home/steamos/games/x")}; !slices.Equal(f.Replace, want) {
		t.Errorf("home folder replacement %+v, want %+v", f.Replace, want)
	}
	if f := find(rep, CodeBroadFolder, 2); f == nil || f.Level != LevelWarning {
		t.Errorf("broad folder: %+v", f)
	}
	if f := find(rep, CodeHostWide, 4); f == nil || !slices.Equal(f.Replace, []config.ProjectMatch{remote("github.com/someone/thing")}) {
		t.Errorf("host wide: %+v", f)
	}
	// A temporary folder a device still holds: removed, and what it
	// admitted hidden from review.
	if f := find(rep, CodeTemporary, 5); f == nil || f.Fix != FixNarrow || !slices.Equal(f.Hide, []catalog.ProjectKey{{Kind: catalog.KeyCWD, Key: "/tmp/t3code-title-a"}}) {
		t.Errorf("held temporary: %+v", f)
	}
	if f := find(rep, CodeTemporary, 6); f == nil || f.Fix != FixSafe {
		t.Errorf("gone temporary: %+v", f)
	}
	if f := find(rep, CodeUnmatched, 7); f == nil || f.Fix != FixRemove {
		t.Errorf("unmatched: %+v", f)
	}
	if find(rep, CodeUnmatched, 0) != nil || find(rep, CodeCovered, 0) != nil {
		t.Error("a rule in use is flagged")
	}
	if rep.Pending != 3 {
		t.Errorf("pending %d, want 3", rep.Pending)
	}
	if f := find(rep, CodeReviewGroup, -1); f == nil || f.Group != "/home/me/.vuoro/wt" || f.Count != 3 {
		t.Errorf("review group: %+v", f)
	}
	for i := 1; i < len(rep.Findings); i++ {
		if rank(rep.Findings[i].Level) < rank(rep.Findings[i-1].Level) {
			t.Errorf("findings out of order at %d: %s after %s", i, rep.Findings[i].Level, rep.Findings[i-1].Level)
		}
	}

	safe, n := ApplySafe(in.Projects, rep)
	if want := []config.ProjectMatch{allow[0], allow[2], allow[3], allow[4], allow[5], allow[7]}; n != 3 || !slices.Equal(safe.Allow, want) {
		t.Errorf("ApplySafe: %d %+v, want 3 %+v", n, safe.Allow, want)
	}
	if !slices.Equal(safe.Deny, deny) {
		t.Error("ApplySafe changed the deny rules")
	}
	if _, n := ApplyUnmatched(in.Projects, rep); n != 4 {
		t.Errorf("ApplyUnmatched removed %d, want 4", n)
	}
	narrowed, hide, ok := ApplyNarrow(in.Projects, *find(rep, CodeHomeFolder, 3))
	if !ok || hide != nil || len(narrowed.Allow) != len(allow)+1 || narrowed.Allow[3] != (config.ProjectMatch{CWDHash: "0123456789abcdef"}) {
		t.Errorf("ApplyNarrow: %v %+v", ok, narrowed.Allow)
	}
	stale := *find(rep, CodeHomeFolder, 3)
	stale.Index = 2
	if _, _, ok := ApplyNarrow(in.Projects, stale); ok {
		t.Error("ApplyNarrow applied a finding for another rule")
	}
}

func rank(level string) int {
	return slices.Index([]string{LevelProblem, LevelWarning, LevelTidy, LevelOffer, LevelInfo}, level)
}

// A device with no inventory leaves the doctor unsure what a rule
// matches there, so a temporary rule is no longer safe to drop.
// Root's home on macOS is a home folder, though no home or Users folder
// holds it.
func TestDiagnoseRootHome(t *testing.T) {
	rep := Diagnose(Input{Projects: config.Projects{Allow: []config.ProjectMatch{cwd("/var/root")}}})
	if f := find(rep, CodeHomeFolder, 0); f == nil {
		t.Errorf("/var/root is not a home folder: %+v", rep.Findings)
	}
}

func TestDiagnoseUnlisted(t *testing.T) {
	in := Input{Projects: config.Projects{Allow: []config.ProjectMatch{cwd("/tmp/x/y")}},
		Devices: []Device{{Name: "laptop", Listed: true}, {Name: "mac"}}}
	rep := Diagnose(in)
	if f := find(rep, CodeTemporary, 0); f == nil || f.Fix != FixNarrow {
		t.Errorf("temporary with an unlisted device: %+v", f)
	}
	if !slices.Equal(rep.Unlisted, []string{"mac"}) || find(rep, CodeUnlisted, -1) == nil {
		t.Errorf("unlisted %v", rep.Unlisted)
	}
	if rep.Safe() != 0 {
		t.Errorf("%d safe fixes", rep.Safe())
	}
}

func TestDiagnoseCap(t *testing.T) {
	var allow []config.ProjectMatch
	for i := range RuleCap {
		allow = append(allow, remote(fmt.Sprintf("h%d/o/r", i)))
	}
	if find(Diagnose(Input{Projects: config.Projects{Allow: allow}}), CodeAtCap, -1) == nil {
		t.Error("no at_cap at the cap")
	}
	if find(Diagnose(Input{Projects: config.Projects{Allow: allow[:RuleCap*nearCapPercent/100]}}), CodeNearCap, -1) == nil {
		t.Error("no near_cap at the threshold")
	}
	if f := Diagnose(Input{Projects: config.Projects{Allow: allow[:10]}}); find(f, CodeNearCap, -1) != nil || find(f, CodeAtCap, -1) != nil {
		t.Error("a cap finding for ten rules")
	}
	// A short list with a long queue is near the cap too.
	var queue []protocol.InventoryProject
	for i := range RuleCap {
		queue = append(queue, project(fmt.Sprintf("h%d/q", i), "/q", false))
	}
	if find(Diagnose(Input{Projects: config.Projects{Allow: allow[:10]}, Devices: []Device{{Name: "d", Listed: true, Projects: queue}}}), CodeNearCap, -1) == nil {
		t.Error("no near_cap for a queue that would pass the cap")
	}
}

func TestTemporary(t *testing.T) {
	for dir, want := range map[string]bool{
		"/tmp/t3code-claude-title-8CzU0i":                                                true,
		"/var/folders/b1/8b_cqz6d75d5h28vtz0_dzjr0000gn/T/TestLiveMCPSmoke527642525/001": true,
		"/home/me/.cache/agent-scratch/terva/x":                                          true,
		"/work/tmp.TC7yLMMc8O":                                                           true,
		"/work/TestThing1234567/001":                                                     true,
		"/tmp":                                                                           false,
		"/home/me/workspace/temp":                                                        false,
		"/home/me/workspace/TestingThings":                                               false,
		"/home/me/.t3/worktrees/lampi/t3-1":                                              false,
	} {
		if got := Temporary(dir); got != want {
			t.Errorf("Temporary(%q) = %v, want %v", dir, got, want)
		}
	}
}
