package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
)

func TestStatusPrintsHarnessLines(t *testing.T) {
	cfg := t.TempDir()
	state := t.TempDir()
	home := t.TempDir()
	tervaHome := t.TempDir()
	claudeHome := t.TempDir()
	codexEnv := t.TempDir()
	codexRoot := t.TempDir()
	xdgData := t.TempDir()

	dir := filepath.Join(cfg, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("{\"harnesses\":{\"claude\":{\"enabled\":false},\"codex\":{\"root\":%q}}}\n", codexRoot)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	getenv := func(k string) string {
		switch k {
		case "HOME":
			return home
		case "XDG_CONFIG_HOME":
			return cfg
		case "XDG_STATE_HOME":
			return state
		case "XDG_DATA_HOME":
			return xdgData
		case "TERVA_HOME":
			return tervaHome
		case "CLAUDE_CONFIG_DIR":
			return claudeHome
		case "CODEX_HOME":
			return codexEnv
		default:
			return ""
		}
	}

	var out bytes.Buffer
	err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: getenv,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, label := range []string{
		"terva_home:",
		"claude_config_dir:",
		"codex_home:",
		"opencode_data_dir:",
		"cursor_user_data:",
		"cursor_cli_config:",
	} {
		if strings.Contains(text, label) {
			t.Fatalf("old home line %q still present\n%s", label, text)
		}
	}
	for _, want := range []string{"machine_id: ", "sessions: ", "outbox: "} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q\n%s", want, text)
		}
	}

	cursorHome, err := cursor.Adapter{}.Home(getenv)
	if err != nil {
		t.Fatal(err)
	}
	cliHome, err := cursorcli.Adapter{}.Home(getenv)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		fmt.Sprintf("harness terva enabled=true root=%s source=env", tervaHome),
		fmt.Sprintf("harness claude enabled=false root=%s source=env", claudeHome),
		fmt.Sprintf("harness codex enabled=true root=%s source=config", codexRoot),
		fmt.Sprintf("harness opencode enabled=true root=%s source=env", filepath.Join(xdgData, "opencode")),
		fmt.Sprintf("harness cursor enabled=true root=%s source=default", cursorHome),
		fmt.Sprintf("harness cursor-cli enabled=true root=%s source=default", cliHome),
	}
	lines := harnessLines(text)
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("harness lines:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func harnessLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "harness ") {
			out = append(out, line)
		}
	}
	return out
}
