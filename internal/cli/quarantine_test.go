package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/redact"
	"terva.sh/lampi/internal/upload"
)

func TestQuarantineListAndAllow(t *testing.T) {
	state := t.TempDir()
	getenv := statusEnv(t.TempDir(), t.TempDir(), state)
	dir := filepath.Join(state, "terva-lampi")
	file := redact.Record{RelPath: "sessions/a/s.jsonl", SHA256: strings.Repeat("a", 64), Ruleset: "v2", Hits: 1, Rules: []string{"aws-access-key-id"}, At: time.Unix(1, 0)}
	grown := file
	grown.SHA256 = strings.Repeat("b", 64)
	grown.At = time.Unix(2, 0)
	manifest := redact.Record{RelPath: "sessions/b/t.jsonl", SHA256: strings.Repeat("c", 64), Ruleset: "v2", Hits: 1, Rules: []string{"github-token"}, Manifest: true}
	for _, r := range []redact.Record{file, grown, manifest} {
		if err := redact.AppendQuarantine(dir, r); err != nil {
			t.Fatal(err)
		}
	}

	var out, errb bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errb, Getenv: getenv}
	// A relpath allows its newest digest only.
	if err := Run([]string{"quarantine", "allow", "sessions/a/s.jsonl"}, env); err != nil {
		t.Fatal(err)
	}
	got, err := redact.AllowedDigests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[grown.SHA256] {
		t.Fatalf("allowed %v", got)
	}
	if err := Run([]string{"quarantine", "allow", manifest.SHA256}, env); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("manifest hit allowed: %v", err)
	}
	if err := Run([]string{"quarantine", "allow", "sessions/none.jsonl"}, env); err == nil {
		t.Fatal("unknown relpath allowed")
	}

	out.Reset()
	if err := Run([]string{"quarantine", "list"}, env); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("list:\n%s", out.String())
	}
	if !strings.HasSuffix(lines[0], "hits=1 rules=aws-access-key-id") ||
		!strings.HasSuffix(lines[1], "rules=aws-access-key-id allowed") ||
		!strings.HasSuffix(lines[2], "rules=github-token manifest") {
		t.Fatalf("list:\n%s", out.String())
	}
}

// The agent prints a refusal the first pass that has it, not on every
// pass after. One that goes away and comes back prints again.
func TestAgentLogsARefusalOnce(t *testing.T) {
	home, cfg, state, _ := agentFixture(t, "http://127.0.0.1:1")
	// Nothing is allowlisted, so the pass refuses before the lake.
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(`{"server":"http://127.0.0.1:1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	env := Env{Stdout: &out, Stderr: &errb, Getenv: agentGetenv(home, cfg, state)}
	opt, _, _, _, err := loadAgent(env, "", "")
	if err != nil {
		t.Fatal(err)
	}
	seen := &changeLog{}
	for range 3 {
		if err := runAgentSync(context.Background(), env, opt, "", seen); err == nil {
			t.Fatal("pass was not refused")
		}
	}
	if n := strings.Count(errb.String(), "not allowlisted"); n != 1 {
		t.Fatalf("refusal printed %d times:\n%s", n, errb.String())
	}
	if got := seen.fresh("refuse", nil); len(got) != 0 {
		t.Fatal(got)
	}
	if err := runAgentSync(context.Background(), env, opt, "", seen); err == nil {
		t.Fatal("pass was not refused")
	}
	if n := strings.Count(errb.String(), "not allowlisted"); n != 2 {
		t.Fatalf("a refusal that came back was not printed:\n%s", errb.String())
	}
}

func TestStatusPrintsLastAttempt(t *testing.T) {
	cfg, home, state := t.TempDir(), t.TempDir(), t.TempDir()
	var out bytes.Buffer
	env := Env{Stdout: &out, Stderr: ioDiscard(), Getenv: statusEnv(cfg, home, state)}
	if err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, env); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "last_attempt: never\nlast_error: none\n") {
		t.Fatalf("status:\n%s", out.String())
	}
	// A pass with a session and an allowlist against a lake that is down.
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), []byte(`{"projects":{"allow":[{"cwd_prefix":"/work/app"}]}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(home, "sessions", "abcd", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(session, []byte(`{"type":"meta","meta":{"id":"s","cwd":"/work/app"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"sync", "--server", "http://127.0.0.1:1"}, env); err == nil {
		t.Fatal("sync to a down lake succeeded")
	}
	a, ok, err := upload.ReadAttempt(filepath.Join(state, "terva-lampi"))
	if err != nil || !ok {
		t.Fatalf("attempt ok=%v err=%v", ok, err)
	}
	out.Reset()
	if err := Run([]string{"status", "--server", "http://127.0.0.1:1"}, env); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "last_attempt: "+a.At.UTC().Format(time.RFC3339)+" failed\n") ||
		!strings.Contains(text, "last_error: "+a.LastErrorAt.UTC().Format(time.RFC3339)+" upload: ") {
		t.Fatalf("status:\n%s", text)
	}
}
