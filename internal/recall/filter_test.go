package recall

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

// TestEventFilterAgreesWithSearch runs the same filters through the
// index and through EventFilter over the published files. Both must
// pick the same events, or export and search would disagree.
func TestEventFilterAgreesWithSearch(t *testing.T) {
	s := lake(t)
	a := ingestAs(t, s, "codex", "a", "git@example.com:org/one.git")
	b := ingestAs(t, s, "claude", "b", "")
	str := func(v string) *string { return &v }
	yes, no := true, false
	build := func(prefix string) []normalize.Event {
		evs := events(40, func(i int) string { return fmt.Sprint(prefix, " event ", i) })
		for i := range evs {
			evs[i].RecordedAt = time.Date(2026, 9, 1+i%10, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
			switch i % 4 {
			case 1:
				evs[i].Actor, evs[i].EventType = normalize.ActorAssistant, normalize.EventToolCall
				evs[i].Tool = normalize.Tool{Name: str("Bash")}
				evs[i].RawType = "function_call"
			case 2:
				evs[i].Actor, evs[i].EventType = normalize.ActorTool, normalize.EventToolResult
				evs[i].Tool = normalize.Tool{Name: str("Bash"), IsError: map[bool]*bool{true: &yes, false: &no}[i%8 == 2]}
				if i%12 == 10 {
					evs[i].Tool.IsError = nil
				}
			case 3:
				evs[i].Actor, evs[i].EventType, evs[i].ContentText = normalize.ActorHarness, normalize.EventUsage, nil
				// A projection with no source time writes ingested_at.
				evs[i].RecordedAt = evs[i].IngestedAt
			}
		}
		return evs
	}
	publish(t, s, a, build("alpha"))
	publish(t, s, b, build("beta"))
	x := openIndex(t, s)
	pass(t, x)

	sessions, err := s.Catalog.ListSessions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	project := protocol.ProjectLinkID("git@example.com:org/one.git", root("x"))
	at := func(day int) *time.Time { v := time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC); return &v }
	for name, req := range map[string]SearchRequest{
		"type":            {EventType: "tool_call"},
		"actor":           {Actor: "harness"},
		"tool":            {ToolName: "Bash"},
		"tool is exact":   {ToolName: "bash"},
		"failed":          {ToolError: &yes},
		"succeeded":       {ToolError: &no},
		"raw type":        {RawType: "function_call", Harness: "claude"},
		"project":         {EventType: "tool_result", Project: project},
		"since":           {Actor: "user", Since: at(5)},
		"until":           {EventType: "tool_call", Until: at(3)},
		"window":          {ToolName: "Bash", Since: at(2), Until: at(8)},
		"no recorded":     {EventType: "usage", Since: at(1)},
		"combined, none":  {EventType: "usage", ToolName: "Bash"},
		"harness, failed": {Harness: "codex", ToolError: &yes},
	} {
		req.Scope, req.Limit = catalog.AllBays(), SearchMaxLimit
		var fromIndex []string
		for {
			p := search(t, x, req)
			for _, h := range p.Items {
				fromIndex = append(fromIndex, fmt.Sprint(h.SessionUID, ":", h.Position))
			}
			if p.NextCursor == "" {
				break
			}
			req.Cursor = p.NextCursor
		}
		f := req.Filter()
		var fromFiles []string
		for _, sess := range sessions {
			if !f.Session(sess.Harness, sess.ProjectID) {
				continue
			}
			body, err := normalize.ReadEventsFile(s.Normalized, sess.UID)
			if err != nil {
				t.Fatal(err)
			}
			for pos, line := range bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n")) {
				if f.Line(line) {
					fromFiles = append(fromFiles, fmt.Sprint(sess.UID, ":", pos))
				}
			}
		}
		slices.Sort(fromIndex)
		slices.Sort(fromFiles)
		if empty := name == "tool is exact" || name == "combined, none" || name == "no recorded"; empty != (len(fromIndex) == 0) {
			t.Errorf("%s: index found %d events", name, len(fromIndex))
		}
		if !slices.Equal(fromIndex, fromFiles) {
			t.Errorf("%s: index %d events %v, filter %d events %v", name, len(fromIndex), fromIndex, len(fromFiles), fromFiles)
		}
	}
}

func TestEventFilterValidateNamesTheParameter(t *testing.T) {
	early, late := time.Unix(10, 0), time.Unix(20, 0)
	for want, f := range map[string]EventFilter{
		"harness":    {Harness: "emacs"},
		"project":    {Project: strings.Repeat("p", 4097)},
		"event_type": {EventType: "nonsense"},
		"actor":      {Actor: "robot"},
		"tool":       {ToolName: strings.Repeat("x", 257)},
		"raw_type":   {RawType: "\xff\xfe"},
		"until":      {Since: &late, Until: &early},
	} {
		err := f.Validate()
		var fe *FilterError
		if !errors.As(err, &fe) || fe.Param != want || !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v, want an invalid %s", f, err, want)
		}
	}
	if err := (EventFilter{EventType: "unreadable", Actor: "tool", Harness: "cursor-cli"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if !(EventFilter{}).IsZero() || (EventFilter{Harness: "codex"}).IsZero() {
		t.Fatal("IsZero")
	}
}

func TestUnreadableLines(t *testing.T) {
	unreadable := EventFilter{EventType: "unreadable"}
	for _, line := range [][]byte{nil, []byte("not json"), {}} {
		if !unreadable.Line(line) || (EventFilter{Actor: "user"}).Line(line) {
			t.Errorf("%q", line)
		}
	}
	if !(EventFilter{Harness: "codex"}).Line([]byte("not json")) {
		t.Fatal("a session filter alone drops an unreadable line")
	}
}

func TestParseFields(t *testing.T) {
	got, err := ParseFields("harness,session_id,tool.name,content_text,usage.cost_usd,extra.cursor.kind")
	if err != nil || len(got) != 6 {
		t.Fatal(got, err)
	}
	if f, err := ParseFields(""); f != nil || err != nil {
		t.Fatal(f, err)
	}
	for _, bad := range []string{"nope", "tool.nope", "tool.name.x", "harness,", ",harness", "extra.", "harness,harness", "Tool.Name"} {
		if _, err := ParseFields(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestFieldsProject(t *testing.T) {
	fs, err := ParseFields("tool.name,harness,extra.k,tool.is_error,model.id,content_text")
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(`{"harness":"claude","tool":{"name":"Bash","call_id":"c1","is_error":null},"model":null,"content_text":"ls -la\n","extra":{"k":{"a": [1, 2]}}}`)
	want := `{"tool.name":"Bash","harness":"claude","extra.k":{"a":[1,2]},"tool.is_error":null,"model.id":null,"content_text":"ls -la\n"}`
	if got := string(fs.Project(line)); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := string(fs.Project([]byte("not json"))); got != `{"tool.name":null,"harness":null,"extra.k":null,"tool.is_error":null,"model.id":null,"content_text":null}` {
		t.Fatal(got)
	}
}
