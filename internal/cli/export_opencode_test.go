package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/protocol"
)

func TestOpenCodeExportEventsShareGPTAndTrajectory(t *testing.T) {
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

	body := opencodeTrainingExport("/work/app", prompt, opaque)
	sum, ack := putOpenCodeManifest(t, h, "sid-opencode", "/work/app", body)
	quiet := []byte(`{"info":{"id":"sid-quiet","directory":"/work/app","title":"quiet title"}}`)
	quietSum, quietAck := putOpenCodeManifest(t, h, "sid-quiet", "/work/app", quiet)
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
		t.Fatal("events export missed the opencode prompt")
	}
	if !bytes.Contains(events, []byte("quiet title")) || !bytes.Contains(events, []byte(opaque)) || !bytes.Contains(events, []byte(`"session_id":"opencode:sid-opencode"`)) {
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
	if rec.SessionUID != ack.SessionUID || rec.RawSHA256 != sum || rec.SessionID != "opencode:sid-opencode" {
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
		if turn.Value == "quiet title" || strings.Contains(turn.Value, "1.2.3") {
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

func opencodeTrainingExport(cwd, prompt, opaque string) []byte {
	at := time.Date(2026, 9, 22, 16, 10, 1, 477000000, time.UTC).UnixMilli()
	ms := strconv.FormatInt(at, 10)
	return []byte(`{"info":{"id":"sid-opencode","directory":"` + cwd + `","version":"1.2.3","title":"pond session","time":{"created":` + ms + `}},"messages":[{"info":{"id":"msg_user","role":"user","time":{"created":` + ms + `}},"parts":[{"type":"text","text":"` + prompt + `"}]},{"info":{"id":"msg_asst","role":"assistant","parentID":"msg_user","modelID":"claude-sonnet-4-6","providerID":"anthropic","time":{"created":` + ms + `}},"parts":[{"type":"text","text":"I'll read the file."},{"type":"tool","callID":"call_1","tool":"read","state":{"status":"completed","input":{"file_path":"main.go"},"output":"package main","title":"Read main.go","metadata":{"encrypted_content":"` + opaque + `"}}},{"type":"compaction","auto":true},{"type":"retry","attempt":1,"error":{"name":"APIError","data":{"message":"sandbox refused"}},"time":{"created":` + ms + `}}]}]}`)
}

func putOpenCodeManifest(t *testing.T, h http.Handler, native, cwd string, body []byte) (string, protocol.ManifestAck) {
	t.Helper()
	sum, _, err := cas.Hash(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, body)
	ack := postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessOpenCode,
		HarnessVersion:  "1",
		NativeSessionID: native,
		Project:         protocol.Project{CWD: cwd},
		Artifacts: []protocol.Artifact{{
			Kind:    protocol.KindTranscriptJSONL,
			RelPath: "export/" + native + ".json",
			Size:    int64(len(body)),
			SHA256:  sum,
		}},
	})
	return sum, ack
}
