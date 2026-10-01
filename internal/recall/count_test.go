package recall

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCounter(t *testing.T) {
	for _, bad := range []string{"content_text", "content_ref", "extra", "extra.cmd", "nope", "tool.name,harness", ""} {
		if _, err := NewCounter(bad); err == nil {
			t.Errorf("counted by %q", bad)
		}
	}
	c, err := NewCounter("tool.name")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`{"tool":{"name":"Bash"}}`, `{"tool":{"name":"Read"}}`, `{"tool":{"name":"Bash"}}`,
		`{"tool":{"name":null}}`, `not json`, `{"tool":{"name":"Edit"}}`, `{"tool":{"name":"Read"}}`, `{"tool":{"name":"Bash"}}`,
	} {
		if err := c.Add([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, r := range c.Rows() {
		got = append(got, string(r))
	}
	want := `{"value":"Bash","count":3} {"value":"Read","count":2} {"value":null,"count":2} {"value":"Edit","count":1}`
	if strings.Join(got, " ") != want {
		t.Fatalf("rows %s", strings.Join(got, " "))
	}

	ids, _ := NewCounter("event_id")
	for i := range CountMaxValues {
		if err := ids.Add(fmt.Appendf(nil, `{"event_id":"e%d"}`, i)); err != nil {
			t.Fatal(i, err)
		}
	}
	if err := ids.Add([]byte(`{"event_id":"e0"}`)); err != nil {
		t.Fatal("a known value was refused at the cap")
	}
	if err := ids.Add([]byte(`{"event_id":"one too many"}`)); !errors.Is(err, ErrTooManyValues) {
		t.Fatalf("past the cap: %v", err)
	}
}
