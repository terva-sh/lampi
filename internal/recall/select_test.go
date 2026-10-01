package recall

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Select reads every ready session that reaches allows, skips one whose
// file went away and says so, and stops at the first error from
// reaches or emit.
func TestSelect(t *testing.T) {
	s := lake(t)
	a := ingest(t, s, "a")
	b := ingest(t, s, "b")
	c := ingest(t, s, "c")
	ingest(t, s, "unpublished")
	for _, uid := range []string{a, b, c} {
		publish(t, s, uid, events(3, func(i int) string { return fmt.Sprint(uid, " ", i) }))
	}
	if err := removeFile(s, c); err != nil {
		t.Fatal(err)
	}
	r := NewReader(s.Catalog, s.Normalized)
	user := EventFilter{Actor: "user"}
	var got []string
	st, err := r.Select(t.Context(), user, Fields{"content_text"}, func(uid string) (bool, error) { return uid != b, nil }, func(line []byte) error {
		got = append(got, string(line))
		return nil
	})
	if err != nil || st.Rows != 3 || st.Sessions != 1 || st.Skipped != 1 || len(got) != 3 || got[0] != `{"content_text":"`+a+` 0"}` {
		t.Fatalf("select %+v %v %q", st, err, got)
	}
	boom := errors.New("boom")
	if _, err := r.Select(t.Context(), user, nil, func(string) (bool, error) { return false, boom }, func([]byte) error { return nil }); !errors.Is(err, boom) {
		t.Fatalf("reaches error: %v", err)
	}
	n := 0
	st, err = r.Select(t.Context(), user, nil, func(string) (bool, error) { return true, nil }, func(line []byte) error {
		n++
		if !strings.Contains(string(line), `"event_type":"message"`) {
			t.Errorf("whole event %s", line)
		}
		return boom
	})
	if !errors.Is(err, boom) || n != 1 || st.Rows != 0 {
		t.Fatalf("emit error: %+v %v after %d", st, err, n)
	}
}

// A line too long to read is counted whatever the filter, because the
// filter cannot see inside it; it is written only when it can match.
func TestSelectCountsOversizedLines(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "big")
	evs := events(2, func(i int) string { return fmt.Sprint("line ", i) })
	big := strings.Repeat("x", MaxLine)
	evs = append(evs, events(1, func(int) string { return big })...)
	publish(t, s, uid, evs)
	r := NewReader(s.Catalog, s.Normalized)
	all := func(string) (bool, error) { return true, nil }
	for name, c := range map[string]struct {
		f      EventFilter
		fields Fields
		rows   int64
	}{
		"type filter skips it":       {EventFilter{EventType: "message"}, nil, 2},
		"unreadable, fields":         {EventFilter{EventType: "unreadable"}, Fields{"content_text"}, 1},
		"unreadable, whole events":   {EventFilter{EventType: "unreadable"}, nil, 0},
		"session filter with fields": {EventFilter{Harness: "codex"}, Fields{"actor"}, 3},
	} {
		var lines []string
		st, err := r.Select(t.Context(), c.f, c.fields, all, func(l []byte) error { lines = append(lines, string(l)); return nil })
		if err != nil || st.Oversized != 1 || st.Rows != c.rows || int64(len(lines)) != c.rows {
			t.Errorf("%s: %+v %v", name, st, err)
		}
		for _, l := range lines {
			if len(l) > 1<<20 {
				t.Errorf("%s: wrote the oversized line", name)
			}
		}
	}
}
