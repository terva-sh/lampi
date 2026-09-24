package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

// Root precedence is not resolved here. The contract for a later caller
// is: flag, if one exists, then config root, then env, then the adapter
// default. No flag exists today. These tests do not read the environment
// and do not exercise discover or watch.

func TestHarnessesOmitMatchesEmpty(t *testing.T) {
	const shared = `"server":"http://lake.example","token_file":"/tmp/token"`
	omit := mustLoadConfig(t, "{"+shared+"}")
	empty := mustLoadConfig(t, "{"+shared+`,"harnesses":{}}`)

	if omit.Server != empty.Server || omit.TokenFile != empty.TokenFile {
		t.Fatalf("omit %+v empty %+v", omit, empty)
	}
	if len(omit.Projects.Allow) != 0 || len(empty.Projects.Allow) != 0 {
		t.Fatal("projects changed")
	}
	if omit.Redaction.UploadHits || empty.Redaction.UploadHits {
		t.Fatal("redaction changed")
	}
	for _, id := range knownHarnessIDs {
		if _, ok := omit.Harnesses[id]; ok {
			t.Fatalf("omit stored %s", id)
		}
		if _, ok := empty.Harnesses[id]; ok {
			t.Fatalf("empty map stored %s", id)
		}
		if !omit.Harnesses.Enabled(id) || !empty.Harnesses.Enabled(id) {
			t.Fatalf("%s should stay default-on", id)
		}
	}
}

func TestHarnessesPartialMapLeavesOtherKeysToday(t *testing.T) {
	abs := absHarnessRoot(t)
	body := mustJSON(t, map[string]any{
		"harnesses": map[string]any{
			"terva":      map[string]any{"root": abs},
			"cursor-cli": map[string]any{"enabled": false},
		},
	})
	f := mustLoadConfig(t, body)

	terva, ok := f.Harnesses["terva"]
	if !ok {
		t.Fatal("terva missing")
	}
	if !terva.Enabled {
		t.Fatal("omitted enabled should be true")
	}
	if terva.Root != abs {
		t.Fatalf("root %q", terva.Root)
	}
	cli, ok := f.Harnesses["cursor-cli"]
	if !ok || cli.Enabled {
		t.Fatalf("cursor-cli %+v present %v", cli, ok)
	}
	for _, id := range []string{"claude", "codex", "opencode", "cursor"} {
		if _, ok := f.Harnesses[id]; ok {
			t.Fatalf("%s should have no entry", id)
		}
		if !f.Harnesses.Enabled(id) {
			t.Fatalf("%s should stay default-on", id)
		}
	}
	if f.Harnesses.Enabled("cursor-cli") {
		t.Fatal("cursor-cli should be off")
	}
}

func TestHarnessesKeepProjectsAndRedaction(t *testing.T) {
	body := `{
		"server": "http://lake.example",
		"unused_top_level": {"allow": true, "token": "dropped"},
		"projects": {
			"allow": [{"cwd_prefix": "/work/app"}],
			"deny": [{"cwd_prefix": "/work/app/secret"}]
		},
		"redaction": {"upload_hits": true},
		"harnesses": {"claude": {"enabled": true}}
	}`
	f := mustLoadConfig(t, body)
	if f.Server != "http://lake.example" {
		t.Fatalf("server %q", f.Server)
	}
	if !f.Redaction.UploadHits {
		t.Fatal("upload_hits dropped")
	}
	if !f.Projects.Permitted(ProjectID{CWD: "/work/app"}) {
		t.Fatal("allow rule dropped")
	}
	if f.Projects.Permitted(ProjectID{CWD: "/work/app/secret"}) {
		t.Fatal("deny rule dropped")
	}
	if !f.Harnesses["claude"].Enabled {
		t.Fatal("claude should be on")
	}
}

func TestHarnessKnownKeys(t *testing.T) {
	if protocol.HarnessTerva != "terva" ||
		protocol.HarnessClaude != "claude" ||
		protocol.HarnessCodex != "codex" ||
		protocol.HarnessOpenCode != "opencode" ||
		protocol.HarnessCursor != "cursor" ||
		protocol.HarnessCursorCLI != "cursor-cli" {
		t.Fatal("protocol harness ids drifted from the config allowlist")
	}
	for _, id := range knownHarnessIDs {
		body := mustJSON(t, map[string]any{
			"harnesses": map[string]any{
				id: map[string]any{},
			},
		})
		f := mustLoadConfig(t, body)
		entry, ok := f.Harnesses[id]
		if !ok {
			t.Fatalf("%s rejected", id)
		}
		if !entry.Enabled || entry.Root != "" {
			t.Fatalf("%s %+v", id, entry)
		}
		if len(f.Harnesses) != 1 {
			t.Fatalf("%s map %#v", id, f.Harnesses)
		}
	}
}

func TestHarnessUnknownKey(t *testing.T) {
	for _, id := range []string{"cursor_cli", "Cursor", "openai", "claude-code"} {
		body := mustJSON(t, map[string]any{
			"harnesses": map[string]any{
				id: map[string]any{"enabled": true},
			},
		})
		_, err := loadConfigJSON(t, body)
		if err == nil {
			t.Fatalf("%s was accepted", id)
		}
		if !strings.Contains(err.Error(), `"`+id+`"`) {
			t.Fatalf("%s: error %q does not name the key", id, err)
		}
	}
}

func TestHarnessEntryRejectsUnknownFields(t *testing.T) {
	fields := []string{"allowlist", "allow", "deny", "secret", "token", "api_key", "path", "foo"}
	for _, field := range fields {
		var value any = "nope"
		if field == "foo" {
			value = 1
		}
		body := mustJSON(t, map[string]any{
			"harnesses": map[string]any{
				"terva": map[string]any{field: value},
			},
		})
		_, err := loadConfigJSON(t, body)
		if err == nil {
			t.Fatalf("%s was accepted", field)
		}
		if !strings.Contains(err.Error(), `"`+field+`"`) {
			t.Fatalf("%s: error %q does not name the field", field, err)
		}
	}
}

func TestHarnessEnabledOmitAndFalse(t *testing.T) {
	omitted := mustLoadConfig(t, `{"harnesses":{"claude":{}}}`)
	if !omitted.Harnesses["claude"].Enabled {
		t.Fatal("omitted enabled should be true")
	}

	off := mustLoadConfig(t, `{"harnesses":{"opencode":{"enabled":false}}}`)
	if off.Harnesses["opencode"].Enabled {
		t.Fatal("enabled false loaded as true")
	}
	again := roundTripConfig(t, off)
	if again.Harnesses["opencode"].Enabled {
		t.Fatal("enabled false did not round-trip")
	}
	if again.Harnesses["opencode"].Root != "" {
		t.Fatalf("root %q", again.Harnesses["opencode"].Root)
	}
}

func TestHarnessAbsoluteRootRoundTrip(t *testing.T) {
	abs := absHarnessRoot(t)
	body := mustJSON(t, map[string]any{
		"harnesses": map[string]any{
			"cursor": map[string]any{
				"enabled": true,
				"root":    abs,
			},
		},
	})
	f := mustLoadConfig(t, body)
	if !f.Harnesses["cursor"].Enabled || f.Harnesses["cursor"].Root != abs {
		t.Fatalf("loaded %+v", f.Harnesses["cursor"])
	}
	again := roundTripConfig(t, f)
	if !again.Harnesses["cursor"].Enabled || again.Harnesses["cursor"].Root != abs {
		t.Fatalf("round-trip %+v", again.Harnesses["cursor"])
	}
}

func TestHarnessRootEdges(t *testing.T) {
	abs := absHarnessRoot(t)
	ok := mustLoadConfig(t, mustJSON(t, map[string]any{
		"harnesses": map[string]any{
			"codex": map[string]any{"root": abs},
		},
	}))
	if ok.Harnesses["codex"].Root != abs || !ok.Harnesses["codex"].Enabled {
		t.Fatalf("absolute %+v", ok.Harnesses["codex"])
	}

	_, err := loadConfigJSON(t, `{"harnesses":{"codex":{"root":""}}}`)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty root: %v", err)
	}

	for _, rel := range []string{".", "../x", "foo/bar"} {
		body := mustJSON(t, map[string]any{
			"harnesses": map[string]any{
				"codex": map[string]any{"root": rel},
			},
		})
		_, err := loadConfigJSON(t, body)
		if err == nil {
			t.Fatalf("%s was accepted", rel)
		}
		if !strings.Contains(err.Error(), "absolute") || !strings.Contains(err.Error(), rel) {
			t.Fatalf("%s: %v", rel, err)
		}
	}
}

var knownHarnessIDs = []string{
	"terva",
	"claude",
	"codex",
	"opencode",
	"cursor",
	"cursor-cli",
}

func absHarnessRoot(t *testing.T) string {
	t.Helper()
	var root string
	if runtime.GOOS == "windows" {
		root = `C:\no\such\lampi\harness`
	} else {
		root = "/no/such/lampi/harness"
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("%q is not absolute on %s", root, runtime.GOOS)
	}
	return root
}

func loadConfigJSON(t *testing.T, body string) (File, error) {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "terva-lampi")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return LoadFile(func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return dir
		}
		return ""
	})
}

func mustLoadConfig(t *testing.T, body string) File {
	t.Helper()
	f, err := loadConfigJSON(t, body)
	if err != nil {
		t.Fatalf("load: %v\n%s", err, body)
	}
	return f
}

func roundTripConfig(t *testing.T, f File) File {
	t.Helper()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loadConfigJSON(t, string(b))
	if err != nil {
		t.Fatalf("round-trip: %v\n%s", err, b)
	}
	return got
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
