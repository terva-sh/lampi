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

func TestCodexExportEventsShareGPTAndTrajectory(t *testing.T) {
	aws := "AKIA" + "Z2X5QW7RT3LK9PMN"
	opaque := "gAAAAABopaque=="
	prompt := "please use " + aws + " to read the pond"

	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()

	body := codexTrainingTranscript("/work/app", prompt, opaque)
	sum, ack := putCodexManifest(t, h, "sid-codex", "/work/app", body, true)
	quiet := []byte(`{"timestamp":"2026-09-22T16:10:00Z","type":"session_meta","payload":{"id":"sid-quiet","cwd":"/work/app","cli_version":"0.121.0"}}` + "\n")
	quietSum, quietAck := putCodexManifest(t, h, "sid-quiet", "/work/app", quiet, false)
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
		t.Fatal("events export missed the codex prompt")
	}
	if !bytes.Contains(events, []byte("quiet")) || !bytes.Contains(events, []byte(opaque)) || !bytes.Contains(events, []byte(`"session_id":"codex:sid-codex"`)) {
		t.Fatalf("events export:\n%s", events)
	}
	if bytes.Contains(events, []byte("history-only pond")) || bytes.Contains(events, []byte("[redacted:")) {
		t.Fatalf("events export was rewritten or included history:\n%s", events)
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
	if bytes.Contains(shareRaw, []byte("history-only pond")) || bytes.Contains(shareRaw, []byte(aws)) || bytes.Contains(shareRaw, []byte("on-request")) {
		t.Fatalf("training view kept history, a secret, or a meta row:\n%s", shareRaw)
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
	if rec.SessionUID != ack.SessionUID || rec.RawSHA256 != sum || rec.SessionID != "codex:sid-codex" {
		t.Fatalf("lineage %+v", rec)
	}
	var sawHuman, sawCipher, sawTool, sawResult, sawCompact, sawError bool
	for _, turn := range rec.Conversations {
		if strings.Contains(turn.Value, opaque) || strings.Contains(turn.Value, aws) {
			t.Fatalf("value kept ciphertext or a secret: %s", turn.Value)
		}
		if turn.From == "human" && strings.Contains(turn.Value, "[redacted:aws-access-key-id]") {
			sawHuman = true
		}
		if turn.Name == "exec_command" && strings.Contains(turn.Value, "main.go") {
			sawTool = true
		}
		if turn.Value == "package main" {
			sawResult = true
		}
		if turn.Value == "summary of the pond" {
			sawCompact = true
		}
		if turn.Value == "sandbox refused" {
			sawError = true
		}
		if turn.EncryptedContent != nil {
			s, ok := turn.EncryptedContent.(string)
			if !ok || s != opaque {
				t.Fatalf("ciphertext %#v", turn.EncryptedContent)
			}
			sawCipher = true
		}
		if turn.Value == "sid-quiet" || strings.Contains(turn.Value, "used_percent") || strings.Contains(turn.Value, "future_queue") {
			t.Fatalf("non-training row exported: %+v", turn)
		}
	}
	if !sawHuman || !sawCipher || !sawTool || !sawResult || !sawCompact || !sawError {
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

func codexTrainingTranscript(cwd, prompt, opaque string) []byte {
	lines := []string{
		`{"timestamp":"2026-09-22T16:10:00.100Z","type":"session_meta","payload":{"id":"sid-codex","cwd":"` + cwd + `","cli_version":"0.121.0","source":"cli","model_provider":"openai"}}`,
		`{"timestamp":"2026-09-22T16:10:00.500Z","type":"turn_context","payload":{"cwd":"` + cwd + `","model":"gpt-5.4","approval_policy":"on-request"}}`,
		`{"timestamp":"2026-09-22T16:10:01.477Z","type":"event_msg","payload":{"type":"user_message","message":"` + prompt + `"},"future_field":{"n":1}}`,
		`{"timestamp":"2026-09-22T16:10:02.000Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"look at the pond"}],"encrypted_content":"` + opaque + `"}}`,
		`{"timestamp":"2026-09-22T16:10:02.100Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","call_id":"call_1","arguments":"{\"cmd\":\"cat main.go\"}"}}`,
		`{"timestamp":"2026-09-22T16:10:02.200Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"package main"}}`,
		`{"timestamp":"2026-09-22T16:10:02.300Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I'll read the file."}]}}`,
		`{"timestamp":"2026-09-22T16:10:02.400Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":3,"cached_input_tokens":1,"output_tokens":2,"reasoning_output_tokens":0,"total_tokens":5}},"rate_limits":{"primary":{"used_percent":1.5}}}}`,
		`{"timestamp":"2026-09-22T16:10:03.000Z","type":"event_msg","payload":{"type":"error","message":"sandbox refused"}}`,
		`{"timestamp":"2026-09-22T16:11:00.000Z","type":"compacted","payload":{"message":"summary of the pond"}}`,
		`{"type":"future_event","future_queue":true}`,
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func putCodexManifest(t *testing.T, h http.Handler, native, cwd string, body []byte, withHistory bool) (string, protocol.ManifestAck) {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	arts := []protocol.Artifact{{
		Kind:    protocol.KindTranscriptJSONL,
		RelPath: "sessions/2026/09/22/rollout-2026-09-22T16-10-00-" + native + ".jsonl",
		Size:    int64(len(body)),
		SHA256:  sum,
	}}
	if withHistory {
		history := []byte(`{"session_id":"not-a-rollout","ts":1710000000,"text":"history-only pond"}` + "\n")
		hsum, _, err := cas.Hash(bytes.NewReader(history))
		if err != nil {
			t.Fatal(err)
		}
		putBlob(t, h, hsum, history)
		arts = append(arts, protocol.Artifact{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "history.jsonl",
			Size:    int64(len(history)),
			SHA256:  hsum,
		})
	}
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessCodex,
		HarnessVersion:  "1",
		NativeSessionID: native,
		Project:         protocol.Project{CWD: cwd},
		Artifacts:       arts,
	})
	return sum, ack
}
