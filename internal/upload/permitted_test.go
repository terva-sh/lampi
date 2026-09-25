package upload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// A git_remote deny does not refuse a session whose cwd is known to
// sit outside any checkout. The early hello decision has to agree with
// prepare, or a session prepare lets through is sent without a hello.
func TestAnyPermittedMatchesPrepareForScratchDir(t *testing.T) {
	scratch := t.TempDir()
	opt := Options{Projects: config.Projects{
		Allow: []config.ProjectMatch{{CWDPrefix: scratch}},
		Deny:  []config.ProjectMatch{{GitRemote: "github.com/org/secret"}},
	}}
	m := protocol.Manifest{Project: protocol.Project{CWD: scratch}}
	if !opt.Projects.Permitted(projectID(m)) {
		t.Fatal("prepare refuses the scratch session")
	}
	if !anyPermitted(opt, []adapter.Bundle{{Manifests: []protocol.Manifest{m}}}) {
		t.Fatal("anyPermitted refuses a session prepare allows")
	}
}

// Sync hands the Cursor readers the allowlist, so a refused session is
// not exported. Its manifest still reaches prepare to be reported.
func TestBundlesForSkipsRefusedCursorExports(t *testing.T) {
	home := t.TempDir()
	global := filepath.Join(home, "User", "globalStorage", "state.vscdb")
	ws := filepath.Join(home, "User", "workspaceStorage", "ws1", "state.vscdb")
	for _, p := range []string{global, ws} {
		if err := writeCursorDB(p, "hello from cursor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(ws), "workspace.json"), []byte(`{"folder":"file:///work/app"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	opt := Options{
		MachineID:  "machine-1",
		CursorHome: home,
		Projects:   config.Projects{Allow: []config.ProjectMatch{{CWDPrefix: "/work/app"}}},
	}
	bundles, err := bundlesFor(opt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupBundles(bundles)
	if len(bundles) != 1 || len(bundles[0].Manifests) != 2 {
		t.Fatalf("bundles %+v", bundles)
	}
	b := bundles[0]
	for _, m := range b.Manifests {
		a := m.Artifacts[0]
		_, exported := b.Paths[a.RelPath]
		switch m.NativeSessionID {
		case "global":
			if exported || a.SHA256 != "" {
				t.Fatalf("refused global database was exported: %+v", a)
			}
			if !strings.Contains(allowlistRefusal(m), "refused by design") {
				t.Fatalf("refusal %s", allowlistRefusal(m))
			}
		case "workspace/ws1":
			if !exported || a.SHA256 == "" {
				t.Fatalf("allowlisted workspace was not exported: %+v", a)
			}
		default:
			t.Fatalf("session %s", m.NativeSessionID)
		}
	}
}
