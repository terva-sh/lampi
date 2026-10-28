package diagnose

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every code the engine reports has exactly one rule and suggestion in
// the shipped file, and the file names no code the engine lacks.
func TestShippedRulesMatchCodes(t *testing.T) {
	r, err := Parse(suggestionsJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Codes) != len(Codes()) {
		t.Fatalf("%d rules for %d codes", len(r.Codes), len(Codes()))
	}
	for _, c := range Codes() {
		if strings.TrimSpace(r.Codes[c].Suggestion) == "" {
			t.Errorf("%s has no suggestion", c)
		}
	}
}

// The shipped thresholds are the recommended defaults in
// docs/session-diagnostics-calibration.md. A change here is a change to
// that table.
func TestShippedThresholds(t *testing.T) {
	for c, want := range map[Code]map[string]int64{
		LargeToolResult: {"result_bytes": 64 << 10, "count_bytes": 32 << 10, "count": 10},
		LargeToolInput:  {"input_bytes": 32 << 10},
		ErrorStreak:     {"length": 5},
		RetryLoop:       {"window": 5, "retries": 3},
		RepeatedRead:    {"reads": 5},
		ContextPressure: {"compactions": 3, "cache_collapses": 3, "cache_read_floor": 20000, "cache_drop_percent": 50, "context_keep_percent": 80, "idle_seconds": 300},
		OrphanCall:      {"calls": 3},
		OrphanResult:    {"results": 1},
		UnreadableLines: {"lines": 1},
		NoisyToolOrigin: {"share_percent": 25, "min_bytes": 256 << 10},
		UnknownTool:     {"calls": 1},
	} {
		for k, v := range want {
			if got := Shipped.t(c, k); got != v {
				t.Errorf("%s %s = %d, want %d", c, k, got, v)
			}
		}
	}
	for c, sev := range map[Code]Severity{
		LargeToolResult: Warn, LargeToolInput: Info, ErrorStreak: Warn, RetryLoop: Warn, RepeatedRead: Info,
		ContextPressure: Info, OrphanCall: Info, OrphanResult: Info, UnreadableLines: Error, NoisyToolOrigin: Warn, UnknownTool: Warn,
	} {
		if Shipped.Codes[c].Severity != sev {
			t.Errorf("%s severity %s, want %s", c, Shipped.Codes[c].Severity, sev)
		}
	}
}

func TestParseRefuses(t *testing.T) {
	base := func(edit func(m map[string]any)) string {
		var m map[string]any
		if err := json.Unmarshal(suggestionsJSON, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	rule := func(m map[string]any, c Code) map[string]any {
		return m["codes"].(map[string]any)[string(c)].(map[string]any)
	}
	for _, c := range []struct{ want, raw string }{
		{"no rule for", base(func(m map[string]any) { delete(m["codes"].(map[string]any), "retry_loop") })},
		{"unknown code", base(func(m map[string]any) { m["codes"].(map[string]any)["made_up"] = rule(m, RetryLoop) })},
		{"severity", base(func(m map[string]any) { rule(m, RetryLoop)["severity"] = "fatal" })},
		{"no suggestion", base(func(m map[string]any) { rule(m, RetryLoop)["suggestion"] = " " })},
		{"not https", base(func(m map[string]any) { rule(m, RetryLoop)["link"] = "http://x" })},
		{"exactly", base(func(m map[string]any) { rule(m, RetryLoop)["thresholds"] = map[string]any{"window": 5} })},
		{"must be positive", base(func(m map[string]any) { rule(m, RetryLoop)["thresholds"] = map[string]any{"window": 0, "retries": 3} })},
		{"no version", base(func(m map[string]any) { delete(m, "version") })},
		{"unknown field", base(func(m map[string]any) { m["extra"] = 1 })},
		{"data after", string(suggestionsJSON) + "{}"},
		{"exactly", base(func(m map[string]any) { rule(m, RetryLoop)["thresholds"] = map[string]any{"window": 5, "tries": 3} })},
	} {
		if _, err := Parse([]byte(c.raw)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.want, err)
		}
	}
}
