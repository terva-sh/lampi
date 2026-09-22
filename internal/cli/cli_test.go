package cli

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
)

func TestRootHelpListsCommands(t *testing.T) {
	var out bytes.Buffer
	err := Run([]string{"--help"}, Env{Stdout: &out, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, cmd := range []string{"serve", "agent", "sync", "status", "login"} {
		if !strings.Contains(text, "terva-lampi "+cmd) {
			t.Fatalf("help missing %s:\n%s", cmd, text)
		}
	}
	if !strings.Contains(text, "not the\nprimary name") && !strings.Contains(text, "not the primary name") {
		t.Fatalf("help should say lampi is not the primary name:\n%s", text)
	}
	if strings.Contains(text, "primary command is `lampi`") || strings.Contains(text, "primary binary is lampi") {
		t.Fatalf("help treats bare lampi as primary:\n%s", text)
	}
}

func TestAliasWarning(t *testing.T) {
	var errb bytes.Buffer
	if err := Run([]string{"--help"}, Env{Stdout: ioDiscard(), Stderr: &errb, Argv0: "/usr/local/bin/lampi"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb.String(), "neurobin") {
		t.Fatalf("stderr: %s", errb.String())
	}
	errb.Reset()
	if err := Run([]string{"--help"}, Env{Stdout: ioDiscard(), Stderr: &errb, Argv0: "terva-lampi"}); err != nil {
		t.Fatal(err)
	}
	if errb.Len() != 0 {
		t.Fatalf("unexpected warning: %s", errb.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	err := Run([]string{"pond"}, Env{Stdout: ioDiscard(), Stderr: ioDiscard()})
	if err == nil || !strings.Contains(err.Error(), "pond") {
		t.Fatalf("err %v", err)
	}
}

func TestLoginDoesNotPrintToken(t *testing.T) {
	cfg := t.TempDir()
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: xdg(cfg),
	}
	if err := Run([]string{"login"}, env); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg, "terva-lampi", "token")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	tok, err := auth.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), tok) {
		t.Fatal("token was printed")
	}
	if err := Run([]string{"login", "super-secret"}, env); err == nil {
		t.Fatal("positional token should be rejected")
	}
}

func TestAgentDiscover(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"agent", "discover"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "sessions/abcd/s.jsonl") {
		t.Fatalf("discover: %s", out.String())
	}
	out.Reset()
	if err := Run([]string{"agent", "machine-id"}, env); err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(out.String())
	if len(id) != 26 {
		t.Fatalf("machine id %q", id)
	}
	out.Reset()
	if err := Run([]string{"agent", "machine-id"}, env); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != id {
		t.Fatal("machine id was not stable")
	}
}

func TestStatusHealth(t *testing.T) {
	lake, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	cfg := t.TempDir()
	home := t.TempDir()
	var out bytes.Buffer
	err = Run([]string{"status", "--server", srv.URL}, Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "XDG_CONFIG_HOME":
				return cfg
			case "TERVA_HOME":
				return home
			default:
				return ""
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "health: ok") {
		t.Fatalf("status:\n%s", out.String())
	}
}

func TestSyncAgainstServe(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	tok := "abc123"
	if err := auth.Write(filepath.Join(data, "token"), tok); err != nil {
		t.Fatal(err)
	}
	lake.Token = tok
	srv := httptest.NewServer(lake.Handler())
	t.Cleanup(srv.Close)

	home := t.TempDir()
	cfg := t.TempDir()
	dir := filepath.Join(home, "sessions", "abcd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\"type\":\"meta\",\"meta\":{\"id\":\"sess-1\",\"cwd\":\"/work/app\"}}\n"
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tokenCopy := filepath.Join(cfg, "token")
	if err := auth.Write(tokenCopy, tok); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	env := Env{
		Stdout: &out,
		Stderr: ioDiscard(),
		Getenv: func(k string) string {
			switch k {
			case "TERVA_HOME":
				return home
			case "XDG_CONFIG_HOME":
				return cfg
			default:
				return ""
			}
		},
	}
	if err := Run([]string{"sync", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "uploaded 1") {
		t.Fatalf("sync: %s", out.String())
	}
	out.Reset()
	if err := Run([]string{"sync", "--server", srv.URL, "--token-file", tokenCopy}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "uploaded 0") {
		t.Fatalf("resync: %s", out.String())
	}
}

func xdg(cfg string) func(string) string {
	return func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return cfg
		}
		return ""
	}
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
