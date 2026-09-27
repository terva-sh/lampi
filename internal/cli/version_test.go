package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildInfoVersion(t *testing.T) {
	for _, tc := range []struct {
		name      string
		info      debug.BuildInfo
		ver, comm string
	}{
		{"tagged release", debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "false"}}}, "v0.1.0", "0123456789ab"},
		{"modified tree", debug.BuildInfo{Main: debug.Module{Version: "v0.1.1-0.20260927000000-0123456789ab+dirty"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"}, {Key: "vcs.modified", Value: "true"}}}, "v0.1.1-0.20260927000000-0123456789ab+dirty", "0123456789ab-modified"},
		{"devel without vcs", debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "0.0.0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, c := buildInfoVersion(&tc.info)
			if v != tc.ver || c != tc.comm {
				t.Fatalf("got %q %q, want %q %q", v, c, tc.ver, tc.comm)
			}
		})
	}
}

// A release is go build in a checkout at its tag, with nothing linked
// in. This builds the binary that way from a clone of this repository
// and checks that --version names the tag, which is what both release
// workflows require before publishing.
func TestTaggedBuildReportsItsTag(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
	root := repoRoot(t)
	if err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Run(); err != nil {
		t.Skip("not a git checkout")
	}
	clone := filepath.Join(t.TempDir(), "lampi")
	run := func(dir string, env []string, name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", nil, "git", "clone", "--quiet", "--no-local", "--depth", "1", "file://"+root, clone)
	// An unlikely tag, so it cannot collide with a real one.
	run(clone, nil, "git", "tag", "v0.0.99-buildinfo")
	bin := filepath.Join(t.TempDir(), "terva-lampi")
	env := []string{"CGO_ENABLED=0", "GOFLAGS=-buildvcs=true", "GOWORK=off"}
	run(clone, env, "go", "build", "-trimpath", "-o", bin, "./cmd/terva-lampi")
	got := run("", nil, bin, "--version")
	if !strings.HasPrefix(got, "terva-lampi v0.0.99-buildinfo (") || strings.Contains(got, "modified") {
		t.Fatalf("--version is %q, want the tag and an unmodified commit", got)
	}
}
