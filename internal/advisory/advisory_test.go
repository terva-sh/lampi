package advisory

import (
	"strings"
	"testing"
)

// The file this build ships must parse, or the lake would not start.
func TestShippedAdvisoriesParse(t *testing.T) {
	if _, err := Parse(agentsJSON); err != nil {
		t.Fatal(err)
	}
}

func TestMatchRanges(t *testing.T) {
	s, err := Parse([]byte(`{"agents": [
		{"introduced": "v0.1.0", "fixed": "v0.1.3", "severity": "upgrade", "reason": "slow uploads"},
		{"introduced": "v0.1.1", "fixed": "v0.1.2", "severity": "urgent", "reason": "drops sessions", "link": "https://example.test/note"},
		{"introduced": "v0.3.0", "severity": "upgrade", "reason": "open-ended"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	for v, want := range map[string]string{
		"v0.0.9": "", "v0.1.0": "slow uploads", "v0.1.1": "drops sessions", "0.1.2": "slow uploads",
		"v0.1.3": "", "v0.2.9": "", "v0.3.0": "open-ended", "v1.0.0": "open-ended",
		"0.0.0": "", "(devel)": "", "": "", "v0.1.1-rc.1": "",
	} {
		a, ok := s.Match(v)
		if ok != (want != "") || a.Reason != want {
			t.Errorf("%q: %q %v, want %q", v, a.Reason, ok, want)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	for raw, want := range map[string]string{
		`{"agents": [{"introduced": "0.0.0", "severity": "urgent", "reason": "x"}]}`:                      "not a release",
		`{"agents": [{"introduced": "v0.2.0", "fixed": "v0.1.0", "severity": "urgent", "reason": "x"}]}`:  "not after",
		`{"agents": [{"introduced": "v0.2.0", "fixed": "v0.2.0", "severity": "urgent", "reason": "x"}]}`:  "not after",
		`{"agents": [{"introduced": "v0.2.0", "severity": "bad", "reason": "x"}]}`:                        "severity",
		`{"agents": [{"introduced": "v0.2.0", "severity": "urgent", "reason": " "}]}`:                     "no reason",
		`{"agents": [{"introduced": "v0.2.0", "severity": "urgent", "reason": "x", "link": "http://a"}]}`: "not https",
		`{"agents": [{"introduced": "v0.2.0", "severity": "urgent", "reason": "x", "extra": 1}]}`:         "unknown field",
	} {
		if _, err := Parse([]byte(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", raw, err, want)
		}
	}
}
