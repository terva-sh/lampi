package normalize

import (
	"context"
	"strings"
	"testing"
)

// TestUnreadableLinesAreCapped: up to maxUnreadable lines that are not
// JSON objects become markers; one more fails the file, as a file of
// random bytes would, without projecting a marker per line.
func TestUnreadableLinesAreCapped(t *testing.T) {
	type projector interface {
		Normalize(context.Context, []byte) ([]Event, error)
	}
	for name, p := range map[string]projector{
		"claude":  Claude{},
		"codex":   Codex{},
		"terva":   Terva{},
		"grok":    Grok{},
		"grokbot": GrokBot{},
	} {
		good := map[string]string{
			"claude":  `{"type":"user"}`,
			"codex":   `{"type":"session_meta"}`,
			"terva":   `{"type":"meta"}`,
			"grok":    `{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hi"}}}}`,
			"grokbot": `{"role":"user","message":{"content":[{"type":"text","text":"hi"}]}}`,
		}[name]
		file := func(bad int) []byte {
			return []byte(good + "\n" + strings.Repeat("not-json sk-live-secret\n", bad))
		}
		events, err := p.Normalize(context.Background(), file(maxUnreadable))
		if err != nil {
			t.Fatalf("%s: %d unreadable lines: %v", name, maxUnreadable, err)
		}
		markers := 0
		for _, ev := range events {
			if ev.EventType == EventError && ev.Extra["unreadable_line"] != nil {
				markers++
			}
		}
		if markers != maxUnreadable {
			t.Fatalf("%s: %d markers, want %d", name, markers, maxUnreadable)
		}
		_, err = p.Normalize(context.Background(), file(maxUnreadable+1))
		if err == nil || !strings.Contains(err.Error(), "line 2 is not a JSON object") || !strings.Contains(err.Error(), "more than 64 lines") || strings.Contains(err.Error(), "sk-live-secret") {
			t.Fatalf("%s: %d unreadable lines: %v", name, maxUnreadable+1, err)
		}
	}
}
