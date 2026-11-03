package profiledoctor

import (
	"slices"
	"testing"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func TestDoctorPreservesReviewExceptionsAndExcludesHiddenPending(t *testing.T) {
	allow := []config.ProjectMatch{{CWDPrefix: "/tmp"}, {CWDPrefix: "/tmp/keep"}}
	in := Input{
		Projects: config.Projects{Allow: allow},
		Review:   config.ReviewConfig{Rules: []config.ReviewRule{{CWDPrefix: "/tmp", Action: "hide"}}},
		Devices: []Device{{Name: "laptop", Listed: true, Projects: []protocol.InventoryProject{
			{CWD: "/tmp/hide", CWDs: 1, Reason: config.RefusedNoMatch},
			{CWD: "/tmp/keep", CWDs: 1, Reason: config.RefusedNoMatch},
			{CWD: "/tmp/mixed", CWDs: 2, Reason: config.RefusedNoMatch},
		}}},
	}
	report := Diagnose(in)
	if report.Pending != 2 {
		t.Fatalf("pending=%d, want the visible exception and mixed checkout", report.Pending)
	}
	if slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Index == 1 }) {
		t.Fatal("doctor offered to clean up an explicit visibility exception")
	}
	fixed, _ := ApplySafe(in.Projects, report)
	if !slices.Contains(fixed.Allow, allow[1]) {
		t.Fatal("safe cleanup lost visibility exception")
	}
	// Folder folds must not replace rules whose prefixes keep projects visible.
	in.Projects.Allow = []config.ProjectMatch{{CWDPrefix: "/tmp/work/a"}, {CWDPrefix: "/tmp/work/b"}, {CWDPrefix: "/tmp/work/c"}}
	if len(FolderFolds(in.Projects.Allow)) == 0 {
		t.Fatal("fixture offers no capture-only folder fold")
	}
	report = Diagnose(in)
	if slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.Code == CodeFolderFold }) {
		t.Fatal("doctor offered a fold that would replace explicit review exceptions")
	}
}
