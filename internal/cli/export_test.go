package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"

	_ "modernc.org/sqlite"
)

const proofPrompt = "normalize-proof prompt: lampi-pond-7f3a"

// TestKnownPromptAfterIngest checks the export failure path: a session
// that did not normalize is named on stderr, and its raw blob stays as
// it was. The five-step MVP gate, which queries this same prompt after
// a real sync, is internal/accept.TestMVPAcceptance.
func TestKnownPromptAfterIngest(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	bad := []byte("not-json sk-live-secret\n")
	badSum, _, err := cas.Hash(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, badSum, bad)
	postManifest(t, h, manifest("sid-bad", "sessions/x/sid-bad.jsonl", badSum, int64(len(bad))))

	good := fixturePrompt()
	goodSum, _, err := cas.Hash(bytes.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, goodSum, good)
	ack := postManifest(t, h, manifest("sid-prompt", "sessions/x/sid-prompt.jsonl", goodSum, int64(len(good))))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := lake.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || msg != "" {
		t.Fatalf("normalize_error after a valid transcript: %q", msg)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	// Drop the derived file so export projects from the raw blob again.
	if err := os.Remove(filepath.Join(dir, "normalized", ack.SessionUID+".jsonl")); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "events.jsonl")
	var stderr bytes.Buffer
	if err := Run([]string{"export", "--data", dir, "--out", out}, Env{Stdout: &bytes.Buffer{}, Stderr: &stderr}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stderr.String(), "sk-live-secret") || strings.Contains(stderr.String(), "not-json") {
		t.Fatalf("stderr includes the raw line:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "normalize_error") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	if queryContent(t, out, "%"+proofPrompt+"%") != proofPrompt {
		t.Fatal("query missed the fixture prompt")
	}

	left, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", badSum[:2], badSum[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, bad) {
		t.Fatal("failed session's raw blob changed")
	}
}

func fixturePrompt() []byte {
	return []byte(strings.Join([]string{
		`{"type":"meta","meta":{"id":"sid-prompt","cwd":"/home/drew/src/foo","model":"gpt-5","provider":"openai","started":"2026-09-22T16:10:00Z","version":"0.137.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + proofPrompt + `"}],"time":"2026-09-22T16:10:01Z"}}`,
	}, "\n") + "\n")
}

func manifest(native, rel, sum string, size int64) protocol.Manifest {
	return protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessTerva,
		NativeSessionID: native,
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: rel,
			Size:    size,
			SHA256:  sum,
		}},
	}
}

func putBlob(t *testing.T, h http.Handler, sum string, body []byte) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/v1/blobs/"+sum, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put %d %s", rr.Code, rr.Body)
	}
}

func postManifest(t *testing.T, h http.Handler, m protocol.Manifest) protocol.ManifestAck {
	t.Helper()
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("manifest %d %s", rr.Code, rr.Body)
	}
	var ack protocol.ManifestAck
	if err := json.Unmarshal(rr.Body.Bytes(), &ack); err != nil {
		t.Fatal(err)
	}
	if ack.SessionUID == "" {
		t.Fatal("empty session uid")
	}
	return ack
}

func TestShareGPTExportAllowlistLineageAndOpaque(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	allowedBody := trainingTranscript("/work/app", "allow-prompt", "gAAAAABopaque==")
	allowedSum := putManifest(t, h, "sid-allow", "/work/app", allowedBody)

	deniedBody := trainingTranscript("/secret/other", "deny-prompt", "gAAAAABother==")
	deniedSum := putManifest(t, h, "sid-deny", "/secret/other", deniedBody)

	privateBody := trainingTranscript("/work/app/private", "private-prompt", "")
	privateSum := putManifest(t, h, "sid-private", "/work/app/private", privateBody)

	bad := []byte("not-json sk-live-secret\n")
	badSum, _, err := cas.Hash(bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, badSum, bad)
	badAck := postManifest(t, h, manifestProject("sid-bad", "/work/app", "sessions/x/sid-bad.jsonl", badSum, int64(len(bad))))

	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	allow := []byte(`{"projects":{"allow":[{"cwd_prefix":"/work/app"}],"deny":[{"cwd_prefix":"/work/app/private"}]}}` + "\n")
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), allow, 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "sharegpt.jsonl")
	var stderr bytes.Buffer
	env := Env{Stdout: &bytes.Buffer{}, Stderr: &stderr, Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return cfg
		}
		return ""
	}}
	if err := Run([]string{"export", "--data", dir, "--out", out, "--format", "sharegpt"}, env); err != nil {
		t.Fatal(err)
	}
	traj := filepath.Join(dir, "trajectory.jsonl")
	var stderr2 bytes.Buffer
	env.Stderr = &stderr2
	if err := Run([]string{"export", "--data", dir, "--out", traj, "--format", "trajectory"}, env); err != nil {
		t.Fatal(err)
	}
	shareRaw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	trajRaw, err := os.ReadFile(traj)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shareRaw, trajRaw) {
		t.Fatalf("trajectory diverged from sharegpt:\n%s\n%s", shareRaw, trajRaw)
	}

	lines := splitLines(shareRaw)
	if len(lines) != 1 {
		t.Fatalf("rows %d:\n%s", len(lines), shareRaw)
	}
	var rec struct {
		SessionUID    string `json:"session_uid"`
		RawSHA256     string `json:"raw_sha256"`
		Conversations []struct {
			From             string `json:"from"`
			Value            string `json:"value"`
			Name             string `json:"name"`
			CallID           string `json:"call_id"`
			EncryptedContent any    `json:"encrypted_content"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.RawSHA256 != allowedSum {
		t.Fatalf("raw_sha256 %s", rec.RawSHA256)
	}
	if strings.Contains(string(shareRaw), "deny-prompt") || strings.Contains(string(shareRaw), "private-prompt") || strings.Contains(string(shareRaw), "gAAAAABother==") {
		t.Fatalf("disallowed text in export:\n%s", shareRaw)
	}
	if strings.Contains(string(shareRaw), "sk-live-secret") || strings.Contains(string(shareRaw), "not-json") {
		t.Fatalf("failed transcript leaked:\n%s", shareRaw)
	}
	var human, cipher string
	for _, turn := range rec.Conversations {
		if turn.From == "human" {
			human = turn.Value
		}
		if turn.EncryptedContent != nil {
			s, ok := turn.EncryptedContent.(string)
			if !ok || strings.Contains(turn.Value, "gAAAAABopaque==") {
				t.Fatalf("ciphertext interpreted: %+v", turn)
			}
			cipher = s
		}
	}
	if human != "allow-prompt" || cipher != "gAAAAABopaque==" {
		t.Fatalf("trajectory: %+v", rec.Conversations)
	}
	for _, msg := range []string{"not allowlisted", badAck.SessionUID, "normalize_error"} {
		if !strings.Contains(stderr.String(), msg) {
			t.Fatalf("stderr missing %q:\n%s", msg, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "deny-prompt") || strings.Contains(stderr.String(), "gAAAAABopaque==") || strings.Contains(stderr.String(), "sk-live-secret") {
		t.Fatalf("stderr includes raw text:\n%s", stderr.String())
	}

	for _, item := range []struct {
		sum  string
		body []byte
	}{
		{allowedSum, allowedBody},
		{deniedSum, deniedBody},
		{privateSum, privateBody},
		{badSum, bad},
	} {
		left, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", item.sum[:2], item.sum[2:]))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(left, item.body) {
			t.Fatalf("raw blob %s changed", item.sum)
		}
	}

	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", eventsPath}, Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"allow-prompt", "deny-prompt", "private-prompt"} {
		if !strings.Contains(string(events), want) {
			t.Fatalf("events export dropped %s", want)
		}
	}
}

func TestShareGPTExportStripsTrainingTextOnly(t *testing.T) {
	aws := "AKIAIOSFODNN7EXAMPLE"
	github := "ghp_" + strings.Repeat("a", 36)
	slack := "xoxb-1234567890-abcdefghij"
	opaque := "gAAAAAB" + aws + "=="
	prompt := "please use " + aws + " and " + github

	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	body := trainingTranscriptWithTool("/work/app", prompt, opaque, slack)
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	ack := postManifest(t, h, manifestProject("sid-secret", "/work/app", "sessions/x/sid-secret.jsonl", sum, int64(len(body))))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, "terva-lampi"), 0o700); err != nil {
		t.Fatal(err)
	}
	allow := []byte(`{"projects":{"allow":[{"cwd_prefix":"/work/app"}]}}` + "\n")
	if err := os.WriteFile(filepath.Join(cfg, "terva-lampi", "config.json"), allow, 0o600); err != nil {
		t.Fatal(err)
	}
	env := Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return cfg
		}
		return ""
	}}

	sharePath := filepath.Join(dir, "sharegpt.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", sharePath, "--format", "sharegpt"}, env); err != nil {
		t.Fatal(err)
	}
	trajPath := filepath.Join(dir, "trajectory.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", trajPath, "--format", "trajectory"}, env); err != nil {
		t.Fatal(err)
	}
	shareRaw, err := os.ReadFile(sharePath)
	if err != nil {
		t.Fatal(err)
	}
	trajRaw, err := os.ReadFile(trajPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shareRaw, trajRaw) {
		t.Fatalf("trajectory diverged from sharegpt:\n%s\n%s", shareRaw, trajRaw)
	}
	if strings.Contains(string(shareRaw), github) || strings.Contains(string(shareRaw), slack) {
		t.Fatalf("training view kept a secret:\n%s", shareRaw)
	}
	if strings.Count(string(shareRaw), aws) != 1 {
		t.Fatalf("aws key count %d, want the ciphertext copy only:\n%s", strings.Count(string(shareRaw), aws), shareRaw)
	}
	if !strings.Contains(string(shareRaw), "[redacted:aws-access-key-id]") || !strings.Contains(string(shareRaw), "[redacted:github-pat]") || !strings.Contains(string(shareRaw), "[redacted:slack-token]") {
		t.Fatalf("placeholders missing:\n%s", shareRaw)
	}

	var rec struct {
		RawSHA256     string `json:"raw_sha256"`
		Conversations []struct {
			Value            string `json:"value"`
			EncryptedContent any    `json:"encrypted_content"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(splitLines(shareRaw)[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.RawSHA256 != sum {
		t.Fatalf("raw_sha256 %s", rec.RawSHA256)
	}
	var sawCipher bool
	for _, turn := range rec.Conversations {
		if strings.Contains(turn.Value, aws) || strings.Contains(turn.Value, github) || strings.Contains(turn.Value, slack) {
			t.Fatalf("value kept a secret: %s", turn.Value)
		}
		if turn.EncryptedContent != nil {
			s, ok := turn.EncryptedContent.(string)
			if !ok || s != opaque {
				t.Fatalf("ciphertext %#v", turn.EncryptedContent)
			}
			sawCipher = true
		}
	}
	if !sawCipher {
		t.Fatal("ciphertext was dropped")
	}

	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", eventsPath}, Env{Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), github) || !strings.Contains(string(events), slack) || !strings.Contains(string(events), prompt) {
		t.Fatalf("events export was stripped:\n%s", events)
	}
	if strings.Contains(string(events), "[redacted:") {
		t.Fatalf("events export was rewritten:\n%s", events)
	}

	normalized, err := os.ReadFile(filepath.Join(dir, "normalized", ack.SessionUID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(normalized), github) || !strings.Contains(string(normalized), slack) {
		t.Fatalf("normalized lake was stripped:\n%s", normalized)
	}
	left, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", sum[:2], sum[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(left, body) {
		t.Fatal("raw blob changed")
	}
}

func trainingTranscriptWithTool(cwd, prompt, opaque, toolSecret string) []byte {
	lines := []string{
		`{"type":"meta","meta":{"id":"sid","cwd":"` + cwd + `","model":"gpt-5","provider":"openai","started":"2026-09-22T16:10:00Z","version":"0.137.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + prompt + `"}],"time":"2026-09-22T16:10:01Z"}}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"reasoning","summary":"thinking","encrypted_content":"` + opaque + `"},{"type":"tool_call","id":"call_1","name":"bash","arguments":{"command":"` + toolSecret + `"}}],"time":"2026-09-22T16:10:02Z"}}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func TestShareGPTExportDefaultDeny(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()
	body := trainingTranscript("/work/app", "allow-prompt", "")
	putManifest(t, h, "sid-allow", "/work/app", body)
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "sharegpt.jsonl")
	var stderr bytes.Buffer
	env := Env{Stdout: &bytes.Buffer{}, Stderr: &stderr, Getenv: func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return t.TempDir()
		}
		return ""
	}}
	if err := Run([]string{"export", "--data", dir, "--out", out, "--format", "sharegpt"}, env); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(got)) != 0 {
		t.Fatalf("default deny wrote %s", got)
	}
	if !strings.Contains(stderr.String(), "not allowlisted") || strings.Contains(stderr.String(), "allow-prompt") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	if err := Run([]string{"export", "--format", "nope"}, env); err == nil || !strings.Contains(err.Error(), "unknown export format") {
		t.Fatalf("format error: %v", err)
	}
}

func trainingTranscript(cwd, prompt, opaque string) []byte {
	lines := []string{
		`{"type":"meta","meta":{"id":"sid","cwd":"` + cwd + `","model":"gpt-5","provider":"openai","started":"2026-09-22T16:10:00Z","version":"0.137.0"}}`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"` + prompt + `"}],"time":"2026-09-22T16:10:01Z"}}`,
	}
	if opaque != "" {
		lines = append(lines, `{"type":"message","message":{"role":"assistant","content":[{"type":"reasoning","summary":"thinking","encrypted_content":"`+opaque+`"}],"time":"2026-09-22T16:10:02Z"}}`)
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func putManifest(t *testing.T, h http.Handler, native, cwd string, body []byte) string {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	postManifest(t, h, manifestProject(native, cwd, "sessions/x/"+native+".jsonl", sum, int64(len(body))))
	return sum
}

func manifestProject(native, cwd, rel, sum string, size int64) protocol.Manifest {
	m := manifest(native, rel, sum, size)
	m.Project.CWD = cwd
	return m
}

func splitLines(body []byte) [][]byte {
	var out [][]byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		out = append(out, line)
	}
	return out
}

func queryContent(t *testing.T, path, like string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE export_lines (line TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := db.Exec(`INSERT INTO export_lines (line) VALUES (?)`, string(line)); err != nil {
			t.Fatal(err)
		}
	}
	var got string
	err = db.QueryRow(`
		SELECT json_extract(line, '$.content_text')
		FROM export_lines
		WHERE json_extract(line, '$.content_text') LIKE ?`, like).Scan(&got)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	return got
}
