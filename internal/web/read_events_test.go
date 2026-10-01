package web

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
)

// seedHarness files a session for harness with no raw blob, so no
// worker projects over the events a test publishes.
func seedHarness(t *testing.T, lake *api.Server, harness, native string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: harness, NativeSessionID: native, Project: protocol.Project{CWD: "/synthetic"},
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: native + ".jsonl", SHA256: strings.Repeat("b", 64), Size: 12}}}
	ack, err := lake.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

func toolEvents(harness string, calls ...string) []normalize.Event {
	var out []normalize.Event
	for i, c := range calls {
		name, text, _ := strings.Cut(c, " ")
		ev := normalize.Event{SchemaVersion: 1, SessionID: "native", Harness: harness, Actor: normalize.ActorAssistant, EventType: normalize.EventToolCall,
			RecordedAt: time.Date(2026, 9, 1+i, 12, 0, 0, 0, time.UTC).Format(time.RFC3339), IngestedAt: "2026-09-30T00:00:00Z", Tool: normalize.Tool{Name: &name}, ContentText: &text}
		msg := "said " + text
		out = append(out, ev, normalize.Event{SchemaVersion: 1, SessionID: "native", Harness: harness, Actor: normalize.ActorUser, EventType: normalize.EventMessage,
			RecordedAt: ev.RecordedAt, IngestedAt: ev.IngestedAt, ContentText: &msg})
	}
	return out
}

// stream reads an event stream and splits off its end line.
func stream(t *testing.T, w *httptest.ResponseRecorder) ([]string, ReadEventsEnd) {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-ndjson" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("stream %d %v: %s", w.Code, w.Header(), w.Body.String())
	}
	lines := strings.Split(strings.TrimSuffix(w.Body.String(), "\n"), "\n")
	var end map[string]ReadEventsEnd
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &end); err != nil || len(end) != 1 {
		t.Fatalf("no end line: %q", lines[len(lines)-1])
	}
	rows := lines[:len(lines)-1]
	slices.Sort(rows)
	return rows, end[ReadEventsEndKey]
}

// TKT-01M3V3JS: a read token with events:read streams the events a
// filter keeps from the sessions in its scope, the same rows export
// writes, and ends with a line saying it is complete.
func TestReadEventsStream(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	device := strings.Repeat("d", 64)
	lake.Allow(device)
	a := seedHarness(t, lake, "codex", "sid-a")
	b := seedHarness(t, lake, "claude", "sid-b")
	seedHarness(t, lake, "codex", "sid-unpublished")
	publishNormalized(t, lake, a, toolEvents("codex", "Bash git push", "Read README.md"))
	publishNormalized(t, lake, b, toolEvents("claude", "Bash ls"))
	admin := signInAs(t, idp, h, "owners")
	events, _ := mintReadTokenWith(t, h, admin, "agent", "", catalog.PermEventsRead)
	onlyA, _ := mintReadTokenWith(t, h, admin, "one session", a, catalog.PermEventsRead)
	raw := mintReadToken(t, h, admin, "raw", "")

	q := readEventsPath + "?event_type=tool_call&fields=harness,tool.name,content_text"
	rows, end := stream(t, bearer(h, "GET", q, events))
	want := []string{
		`{"harness":"claude","tool.name":"Bash","content_text":"ls"}`,
		`{"harness":"codex","tool.name":"Bash","content_text":"git push"}`,
		`{"harness":"codex","tool.name":"Read","content_text":"README.md"}`,
	}
	if !slices.Equal(rows, want) || !end.Complete || end.Rows != 3 || end.Sessions != 2 || end.Error != "" {
		t.Fatalf("tool calls %q, end %+v", rows, end)
	}

	// Export's selection over the same published files gives the same
	// rows, for whole events and for fields.
	for _, c := range []struct {
		query  string
		filter recall.EventFilter
		fields string
	}{
		{"?tool=Bash", recall.EventFilter{ToolName: "Bash"}, ""},
		{"?harness=codex&actor=user&fields=content_text", recall.EventFilter{Harness: "codex", Actor: "user"}, "content_text"},
		{"?since=2026-09-02&event_type=tool_call", recall.EventFilter{EventType: "tool_call", Since: ptrTime(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))}, ""},
	} {
		got, _ := stream(t, bearer(h, "GET", readEventsPath+c.query, events))
		fields, err := recall.ParseFields(c.fields)
		if err != nil {
			t.Fatal(err)
		}
		var exported []string
		for _, uid := range []string{a, b} {
			harness := map[string]string{a: "codex", b: "claude"}[uid]
			if !c.filter.Session(harness, "") {
				continue
			}
			body, err := normalize.ReadEventsFile(lake.Normalized, uid)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n")) {
				if c.filter.Line(line) {
					if fields != nil {
						line = fields.Project(line)
					}
					exported = append(exported, string(line))
				}
			}
		}
		slices.Sort(exported)
		if len(got) == 0 || !slices.Equal(got, exported) {
			t.Errorf("%s: stream %q, export %q", c.query, got, exported)
		}
	}

	if rows, end := stream(t, bearer(h, "GET", readEventsPath+"?event_type=tool_call&count_by=tool.name", events)); !slices.Equal(rows, []string{`{"value":"Bash","count":2}`, `{"value":"Read","count":1}`}) || end.Rows != 2 || !end.Complete {
		t.Errorf("count by tool: %q %+v", rows, end)
	}
	if rows, end := stream(t, bearer(h, "GET", q, onlyA)); len(rows) != 2 || end.Sessions != 1 || strings.Contains(strings.Join(rows, ""), "claude") {
		t.Errorf("session-scoped token: %q %+v", rows, end)
	}
	for name, c := range map[string]struct {
		token string
		want  int
	}{"raw-only token": {raw, 404}, "device token": {device, 401}, "no token": {"", 401}} {
		if w := bearer(h, "GET", q, c.token); w.Code != c.want {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if w := get(h, q, admin); w.Code != 401 {
		t.Errorf("browser session: %d", w.Code)
	}

	if w := bearer(h, "GET", readEventsPath, events); w.Code != 400 || !strings.Contains(w.Body.String(), "filter_required") {
		t.Errorf("no filter: %d %s", w.Code, w.Body.String())
	}
	if w := bearer(h, "GET", readEventsPath+"?fields=harness", events); w.Code != 400 || !strings.Contains(w.Body.String(), "filter_required") {
		t.Errorf("fields alone: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []string{"?q=push", "?tool=Bash&tool=Read", "?tool=", "?event_type=nonsense", "?tool=Bash&fields=nope", "?tool_error=maybe", "?since=yesterday", "?tool=Bash&fields=", "?tool=Bash&%XX=1", "?tool=Bash;x=1", "?tool=Bash&count_by=content_text", "?tool=Bash&count_by=tool.name&fields=harness", "?tool=Bash&count_by="} {
		if w := bearer(h, "GET", readEventsPath+bad, events); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_request") {
			t.Errorf("%s: %d %s", bad, w.Code, w.Body.String())
		}
	}

	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	s := string(log)
	if !strings.Contains(s, `"kind":"events.read"`) || !strings.Contains(s, `(agent)`) || !strings.Contains(s, "event_type=tool_call") {
		t.Errorf("audit lacks the read:\n%s", s)
	}
	if strings.Contains(s, "git push") || strings.Contains(s, "README.md") || strings.Contains(s, events) {
		t.Error("audit holds content or the secret")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// The stream is never silent for longer than its interval: buffered
// rows are flushed, and an idle stream gets a blank line.
func TestKeepaliveWriter(t *testing.T) {
	idle := httptest.NewRecorder()
	k := newKeepaliveWriter(idle, 5*time.Millisecond)
	time.Sleep(40 * time.Millisecond)
	k.stop()
	if err := k.finish([]byte(`{"end":1}`)); err != nil {
		t.Fatal(err)
	}
	body := idle.Body.String()
	if !strings.HasPrefix(body, "\n") || !strings.HasSuffix(body, "\n"+`{"end":1}`+"\n") || strings.Trim(body, "\n") != `{"end":1}` {
		t.Fatalf("idle stream %q", body)
	}

	busy := httptest.NewRecorder()
	k = newKeepaliveWriter(busy, 5*time.Millisecond)
	if err := k.line([]byte(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	k.stop()
	if !strings.HasPrefix(busy.Body.String(), `{"a":1}`+"\n") || !busy.Flushed {
		t.Fatalf("buffered row not flushed by the interval: %q", busy.Body.String())
	}
}
