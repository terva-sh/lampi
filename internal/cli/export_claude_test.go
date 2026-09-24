package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

func TestClaudeExportEventsShareGPTAndTrajectory(t *testing.T) {
	aws := "AKIAIOSFODNN7EXAMPLE"
	opaque := "gAAAAABopaque=="
	prompt := "please use " + aws + " to read the pond"

	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	body := claudeTrainingTranscript("/work/app", prompt, opaque)
	sum, ack := putClaudeManifest(t, h, "sid-claude", "/work/app", body)
	quiet := []byte(`{"type":"summary","summary":"quiet title","leafUuid":"leaf-quiet","sessionId":"sid-quiet","cwd":"/work/app"}` + "\n")
	quietSum, quietAck := putClaudeManifest(t, h, "sid-quiet", "/work/app", quiet)
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []string{ack.SessionUID, quietAck.SessionUID} {
		msg, ok, err := lake.Catalog.NormalizeError(t.Context(), uid)
		if err != nil || !ok || msg != "" {
			t.Fatalf("normalize_error %s %q ok=%v err=%v", uid, msg, ok, err)
		}
	}
	normalizedPath := filepath.Join(dir, "normalized", ack.SessionUID+".jsonl")
	beforeNorm, err := os.ReadFile(normalizedPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeCAS, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", sum[:2], sum[2:]))
	if err != nil {
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

	eventsPath := filepath.Join(dir, "events.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", eventsPath, "--format", "events"}, env); err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if queryContent(t, eventsPath, "%"+prompt+"%") != prompt {
		t.Fatal("events export missed the claude prompt")
	}
	if !bytes.Contains(events, []byte("quiet title")) || !bytes.Contains(events, []byte(opaque)) || !bytes.Contains(events, []byte(`"session_id":"claude:sid-claude"`)) {
		t.Fatalf("events export:\n%s", events)
	}
	if bytes.Contains(events, []byte("[redacted:")) {
		t.Fatalf("events export was rewritten:\n%s", events)
	}

	var stderr bytes.Buffer
	env.Stderr = &stderr
	sharePath := filepath.Join(dir, "sharegpt.jsonl")
	if err := Run([]string{"export", "--data", dir, "--out", sharePath, "--format", "sharegpt"}, env); err != nil {
		t.Fatal(err)
	}
	trajPath := filepath.Join(dir, "trajectory.jsonl")
	var stderr2 bytes.Buffer
	env.Stderr = &stderr2
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
	lines := splitLines(shareRaw)
	if len(lines) != 1 {
		t.Fatalf("rows %d:\n%s", len(lines), shareRaw)
	}
	if bytes.Contains(shareRaw, []byte("quiet title")) || bytes.Contains(shareRaw, []byte(aws)) {
		t.Fatalf("training view kept a non-turn or a secret:\n%s", shareRaw)
	}
	if !bytes.Contains(shareRaw, []byte("[redacted:aws-access-key-id]")) {
		t.Fatalf("placeholder missing:\n%s", shareRaw)
	}
	var rec struct {
		SessionUID    string `json:"session_uid"`
		SessionID     string `json:"session_id"`
		RawSHA256     string `json:"raw_sha256"`
		Conversations []struct {
			From             string `json:"from"`
			Value            string `json:"value"`
			Name             string `json:"name"`
			EncryptedContent any    `json:"encrypted_content"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(lines[0], &rec); err != nil {
		t.Fatal(err)
	}
	if rec.SessionUID != ack.SessionUID || rec.RawSHA256 != sum || rec.SessionID != "claude:sid-claude" {
		t.Fatalf("lineage %+v", rec)
	}
	var sawHuman, sawCipher bool
	for _, turn := range rec.Conversations {
		if strings.Contains(turn.Value, opaque) || strings.Contains(turn.Value, aws) {
			t.Fatalf("value kept ciphertext or a secret: %s", turn.Value)
		}
		if turn.From == "human" && strings.Contains(turn.Value, "[redacted:aws-access-key-id]") {
			sawHuman = true
		}
		if turn.EncryptedContent != nil {
			s, ok := turn.EncryptedContent.(string)
			if !ok || s != opaque {
				t.Fatalf("ciphertext %#v", turn.EncryptedContent)
			}
			sawCipher = true
		}
		if turn.Value == "quiet title" || strings.Contains(turn.Value, "service_tier") {
			t.Fatalf("non-training row exported: %+v", turn)
		}
	}
	if !sawHuman || !sawCipher {
		t.Fatalf("turns: %+v", rec.Conversations)
	}
	if !strings.Contains(stderr.String(), quietAck.SessionUID) || !strings.Contains(stderr.String(), "no training turns") {
		t.Fatalf("stderr: %s", stderr.String())
	}
	if strings.Contains(stderr.String(), aws) || strings.Contains(stderr.String(), opaque) {
		t.Fatalf("stderr includes raw text:\n%s", stderr.String())
	}

	afterNorm, err := os.ReadFile(normalizedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterNorm, beforeNorm) {
		t.Fatal("normalized JSONL was rewritten")
	}
	afterCAS, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", sum[:2], sum[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterCAS, beforeCAS) || !bytes.Equal(afterCAS, body) {
		t.Fatal("CAS object was rewritten")
	}
	quietCAS, err := os.ReadFile(filepath.Join(dir, "cas", "sha256", quietSum[:2], quietSum[2:]))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(quietCAS, quiet) {
		t.Fatal("quiet session CAS object was rewritten")
	}
}

func claudeTrainingTranscript(cwd, prompt, opaque string) []byte {
	lines := []string{
		`{"type":"user","sessionId":"sid-claude","cwd":"` + cwd + `","version":"2.1.71","gitBranch":"main","timestamp":"2026-09-22T16:10:01.477Z","future_field":{"n":1},"message":{"role":"user","content":"` + prompt + `"}}`,
		`{"type":"assistant","sessionId":"sid-claude","cwd":"` + cwd + `","version":"2.1.71","timestamp":"2026-09-22T16:10:02.100Z","requestId":"req_01","message":{"role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"thinking","thinking":"look at the pond","signature":"EqQBCgsignatureopaque"},{"type":"text","text":"I'll read the file."},{"type":"tool_use","id":"toolu_01","name":"Read","input":{"file_path":"main.go"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_01","content":[{"type":"web_search_result","title":"Pond","url":"https://example.com/pond","encrypted_content":"` + opaque + `","page_age":"2d"}]}],"usage":{"input_tokens":3,"output_tokens":2,"cache_read_input_tokens":1,"cache_creation_input_tokens":0,"service_tier":"standard"}}}`,
		`{"type":"user","sessionId":"sid-claude","cwd":"` + cwd + `","timestamp":"2026-09-22T16:10:03.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","is_error":false,"content":"package main"}]}}`,
		`{"type":"system","subtype":"compact_boundary","content":"summary of the pond","sessionId":"sid-claude","cwd":"` + cwd + `","timestamp":"2026-09-22T16:11:00.000Z"}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func putClaudeManifest(t *testing.T, h http.Handler, native, cwd string, body []byte) (string, protocol.ManifestAck) {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessClaude,
		HarnessVersion:  "1",
		NativeSessionID: native,
		Project:         protocol.Project{CWD: cwd},
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "projects/-work-app/" + native + ".jsonl",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	return sum, ack
}
