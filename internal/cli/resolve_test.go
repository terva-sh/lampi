package cli

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
)

// status, agent config, and sync pick the server the way the agent
// does: LAMPI_SERVER beats config.json. status used to probe the
// config.json URL while the agent uploaded to the env one.
func TestStatusSyncAndAgentConfigShareTheResolver(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	cfg, home, state := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	// config.json names a lake that does not answer.
	raw := `{"server":"http://127.0.0.1:1","token_file":"/nonexistent/token","projects":{"allow":[{"cwd_prefix":"/work/app"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lake.Allow("tok")
	base := statusEnv(cfg, home, state)
	getenv := func(k string) string {
		switch k {
		case "LAMPI_SERVER":
			return srv.URL
		case "LAMPI_TOKEN_FILE":
			return tokenFile
		}
		return base(k)
	}

	var out bytes.Buffer
	if err := Run([]string{"status"}, Env{Stdout: &out, Stderr: ioDiscard(), Getenv: getenv}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{
		fmt.Sprintf("server: %s source=env\n", srv.URL),
		fmt.Sprintf("token_file: %s source=env\n", tokenFile),
		"health: ok",
		"catalog_sessions: 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("status missing %q:\n%s", want, text)
		}
	}

	out.Reset()
	if err := Run([]string{"agent", "config"}, Env{Stdout: &out, Stderr: ioDiscard(), Getenv: getenv}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), fmt.Sprintf("server: %s source=env\n", srv.URL)) {
		t.Fatalf("agent config:\n%s", out.String())
	}

	// Without the env, config.json wins and status says so.
	out.Reset()
	if err := Run([]string{"agent", "config"}, Env{Stdout: &out, Stderr: ioDiscard(), Getenv: base}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"server: http://127.0.0.1:1 source=config\n",
		"token_file: /nonexistent/token source=config\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("agent config missing %q:\n%s", want, out.String())
		}
	}

	// sync reaches the env lake too; the config.json one is down.
	session := filepath.Join(home, "sessions", "abcd", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session, []byte(`{"type":"meta","meta":{"id":"s","cwd":"/work/app"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run([]string{"sync"}, Env{Stdout: &out, Stderr: ioDiscard(), Getenv: getenv}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !strings.Contains(out.String(), "manifests 1") {
		t.Fatalf("sync:\n%s", out.String())
	}
}

// The example units must not pin a URL or a token file: that would
// beat config.json and leave status and the agent disagreeing.
func TestAgentUnitsLeaveServerToConfig(t *testing.T) {
	unit, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "systemd", "terva-lampi-agent.service"))
	if err != nil {
		t.Fatal(err)
	}
	sawEnvFile := false
	for _, line := range strings.Split(string(unit), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Environment=LAMPI_SERVER") || strings.HasPrefix(line, "Environment=LAMPI_TOKEN_FILE") {
			t.Errorf("agent unit pins %s", line)
		}
		if strings.HasPrefix(line, "ExecStart=") && (strings.Contains(line, "--server") || strings.Contains(line, "--token-file")) {
			t.Errorf("agent unit passes a flag: %s", line)
		}
		if line == "EnvironmentFile=-%h/.config/terva-lampi/agent.env" {
			sawEnvFile = true
		}
	}
	if !sawEnvFile {
		t.Error("agent unit lost its EnvironmentFile")
	}
	plist, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "launchd", "sh.terva.lampi.agent.plist"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(plist)
	if strings.Contains(body, "--server") || strings.Contains(body, "--token-file") || strings.Contains(body, "<key>LAMPI_SERVER</key>") {
		t.Errorf("plist pins the server or token file:\n%s", body)
	}
}
