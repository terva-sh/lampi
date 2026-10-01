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
