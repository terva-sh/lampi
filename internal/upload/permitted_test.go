package upload

import (
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
