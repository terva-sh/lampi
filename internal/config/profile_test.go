package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadProfiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProfilesFileName)
	p, err := LoadProfiles(path)
	if err != nil || len(p) != 1 {
		t.Fatalf("missing file: %v %v", p, err)
	}
	if _, ok := p[DefaultProfile]; !ok {
		t.Fatal("missing file has no default profile")
	}
	good := `{"profiles":{"ci":{"harnesses":{"codex":{"enabled":false}},"agent":{"debounce":"2s"},"projects":{"allow":[{"git_remote":"git@x:a/b.git"}],"deny":[{"cwd_prefix":"/secret"}]}}}}`
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err = LoadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 2 || p["ci"].Agent.Debounce != "2s" || p["ci"].Harnesses.Enabled("codex") || len(p["ci"].Projects.Allow) != 1 {
		t.Fatalf("profiles: %+v", p)
	}
	if p["ci"].Version() == p[DefaultProfile].Version() || p["ci"].Version() != p["ci"].Version() {
		t.Fatal("version does not follow content")
	}
	for _, tc := range []struct{ body, want string }{
		{`{"profiles":{"default":{"server":"https://x"}}}`, `unknown field "server"`},
		{`{"profiles":{"default":{"projects":{"allow":[{"cwd":"/x"}]}}}}`, `unknown field "cwd"`},
		{`{"profiles":{"default":{}},"extra":1}`, `unknown field "extra"`},
		{`{"profiles":{"default":{"harnesses":{"codex":{"root":"/etc"}}}}}`, "cannot set a harness root"},
		{`{"profiles":{"default":{"redaction":{"upload_hits":true}}}}`, "cannot upload flagged files"},
		{`{"profiles":{"default":{"agent":{"debounce":"soon"}}}}`, "agent.debounce"},
		{`{"profiles":{"Bad":{}}}`, "a profile name"},
		{`{"profiles":{}} {}`, "trailing data"},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadProfiles(path); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.body, err, tc.want)
		}
	}
}

func TestDeployProfilesExampleLoads(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "deploy", "profiles.json.example")
	p, err := LoadProfiles(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p["ci"]; !ok || len(p[DefaultProfile].Projects.Allow) == 0 {
		t.Fatalf("example profiles: %+v", p)
	}
}
