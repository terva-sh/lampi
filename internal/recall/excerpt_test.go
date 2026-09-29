package recall

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

func TestExcerptRendersBoundedPlainText(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "excerpt")
	bash, yes := "Bash", true
	evs := events(12, func(i int) string { return fmt.Sprint("line ", i) })
	evs[3].Actor, evs[3].EventType, evs[3].Tool = normalize.ActorTool, normalize.EventToolResult, normalize.Tool{Name: &bash, IsError: &yes}
	evil := "<script>alert(1)</script>\x1b[31m red"
	evs[4].ContentText = &evil
	evs[5].Extra = map[string]any{"encrypted_content": "Y2lwaGVy"}
	long := strings.Repeat("é", excerptContentMax)
	evs[6].ContentText = &long
	gen := publish(t, s, uid, evs)
	r := NewReader(s.Catalog, s.Normalized)
	ex, err := r.Excerpt(t.Context(), catalog.AllBays(), uid, ExcerptRequest{From: 2, Count: 5, Gen: gen, Pinned: true, Origin: "https://lake.example"})
	if err != nil {
		t.Fatal(err)
	}
	if ex.From != 2 || ex.To != 7 || ex.Events != 5 || ex.Truncated {
		t.Fatalf("span %+v", ex)
	}
	txt := ex.Text
	for _, want := range []string{
		"Session: excerpt (" + uid + ")", "Events #2 to #6 of generation", "Source: https://lake.example/sessions/" + uid + "/transcript?at=2&gen=",
		"[#3 tool tool_result Bash (error) ", evil, "[encrypted content omitted]", "… content truncated",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("missing %q in\n%s", want, txt[:min(len(txt), 800)])
		}
	}
	if strings.Contains(txt, "Y2lwaGVy") || strings.Contains(txt, "line 7") || strings.Contains(txt, "line 1\n") {
		t.Fatal("opaque content or events outside the span")
	}
	if strings.Index(txt, "[#2 ") > strings.Index(txt, "[#3 ") {
		t.Fatal("order")
	}
	// The byte cap ends the span and says where.
	publish(t, s, uid, events(ExcerptMaxEvents, func(int) string { return strings.Repeat("x", 8<<10) }))
	ex, err = r.Excerpt(t.Context(), catalog.AllBays(), uid, ExcerptRequest{Count: ExcerptMaxEvents})
	if err != nil || !ex.Truncated || ex.Events >= ExcerptMaxEvents || len(ex.Text) > ExcerptBytes+4096 || !strings.Contains(ex.Text, "size limit reached") {
		t.Fatal("byte bound", err, ex.Truncated, ex.Events, len(ex.Text))
	}
	for _, req := range []ExcerptRequest{{Count: 0}, {Count: ExcerptMaxEvents + 1}, {From: -1, Count: 1}, {From: 9999, Count: 1}} {
		if _, err := r.Excerpt(t.Context(), catalog.AllBays(), uid, req); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted %+v: %v", req, err)
		}
	}
	if _, err := r.Excerpt(t.Context(), catalog.AllBays(), uid, ExcerptRequest{Count: 1, Gen: gen, Pinned: true}); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal("stale generation", err)
	}
}
