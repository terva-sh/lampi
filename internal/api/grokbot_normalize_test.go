package api

import (
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/grokbot"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func TestGrokBotProject(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()

	const native = "018f1a2b-3c4d-7e5f-8a9b-0c1d2e3f4a5b"
	rel := "agent-transcripts/" + native + "/" + native + ".jsonl"
	body := transcriptLines(
		`{"role":"user","message":{"content":[{"type":"text","text":"hello "},{"type":"tool_use","name":"Read"},{"type":"text","text":"pond"}]}}`,
		`{"role":"unknown","message":{"content":[{"type":"text","text":"SKIPPED-ROLE"}]}}`,
		`not-a-record`,
		`{"role":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`,
	)
	sum := putBlob(t, h, "", body)
	cwd := "/home/box/agent-data/agent-transcripts/" + native
	m := protocol.Manifest{
		CaptureProtocol: protocol.Version,
		MachineID:       "machine-a",
		Harness:         protocol.HarnessGrokBot,
		HarnessVersion:  grokbot.Version,
		NativeSessionID: native,
		Project:         protocol.Project{CWD: cwd},
		Artifacts: []protocol.Artifact{
			{Kind: protocol.KindTranscriptJSONL, RelPath: rel, Size: int64(len(body)), SHA256: sum},
		},
	}
	ack := postManifest(t, h, m)
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	msg, ok, err := s.Catalog.NormalizeError(t.Context(), ack.SessionUID)
	if err != nil || !ok || msg != "" {
		t.Fatalf("normalize_error %q ok=%v err=%v", msg, ok, err)
	}
	events, err := s.Project(t.Context(), m)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, ev := range events {
		if ev.EventType != normalize.EventMessage {
			continue
		}
		if ev.Harness != protocol.HarnessGrokBot || ev.SessionID != "grokbot:"+native {
			t.Fatalf("event %+v", ev)
		}
		if ev.ContentText != nil {
			texts = append(texts, *ev.ContentText)
		}
	}
	if strings.Join(texts, "|") != "hello pond|done" {
		t.Fatalf("messages %q events %d", texts, len(events))
	}
	uid, arts, found, err := s.Catalog.Current(t.Context(), protocol.HarnessGrokBot, native)
	if err != nil || !found || uid == "" || len(arts) != 1 {
		t.Fatalf("catalog %s found=%v arts=%d err=%v", uid, found, len(arts), err)
	}
}
