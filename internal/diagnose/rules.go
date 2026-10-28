package diagnose

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Code names one kind of finding. Codes are stable: a surface may switch
// on them, and a later calibration changes thresholds, not codes.
type Code string

const (
	LargeToolResult Code = "large_tool_result"
	LargeToolInput  Code = "large_tool_input"
	ErrorStreak     Code = "error_streak"
	RetryLoop       Code = "retry_loop"
	RepeatedRead    Code = "repeated_read"
	ContextPressure Code = "context_pressure"
	OrphanCall      Code = "orphan_call"
	OrphanResult    Code = "orphan_result"
	UnreadableLines Code = "unreadable_lines"
	NoisyToolOrigin Code = "noisy_tool_origin"
	UnknownTool     Code = "unknown_tool"
)

// Severity is how much a finding matters: info is worth knowing, warn
// is likely cost or waste, error is damage to the data or the session.
type Severity string

const (
	Info  Severity = "info"
	Warn  Severity = "warn"
	Error Severity = "error"
)

func (s Severity) rank() int {
	switch s {
	case Error:
		return 2
	case Warn:
		return 1
	}
	return 0
}

// thresholdKeys is the threshold each code reads, and so the exact set
// its entry in the rules file must carry. Sizes are bytes, durations
// seconds, shares percent. The values and their reasons are in
// docs/session-diagnostics-calibration.md, "Recommended defaults".
var thresholdKeys = map[Code][]string{
	// One result of result_bytes or more, or count results of
	// count_bytes or more.
	LargeToolResult: {"result_bytes", "count_bytes", "count"},
	LargeToolInput:  {"input_bytes"},
	// length consecutive results with is_error true.
	ErrorStreak: {"length"},
	// A chain of retries calls, each equal in tool and input to one of
	// the window calls before it and to the chain's call before it,
	// with the same result or after an error. Repeats that do not chain
	// are not a loop, however many a session holds.
	RetryLoop: {"window", "retries"},
	// reads of one path and range with no write to the path between.
	RepeatedRead: {"reads"},
	// compactions, or cache_collapses drops of cache_read below
	// cache_drop_percent of the previous usage when that was over
	// cache_read_floor tokens, while context stayed at
	// context_keep_percent or more, and less than idle_seconds passed.
	ContextPressure: {"compactions", "cache_collapses", "cache_read_floor", "cache_drop_percent", "context_keep_percent", "idle_seconds"},
	OrphanCall:      {"calls"},
	OrphanResult:    {"results"},
	UnreadableLines: {"lines"},
	// One origin holding share_percent or more of a session's tool
	// result bytes and min_bytes or more.
	NoisyToolOrigin: {"share_percent", "min_bytes"},
	UnknownTool:     {"calls"},
}

// Codes is every code this build reports, in a stable order.
func Codes() []Code {
	out := make([]Code, 0, len(thresholdKeys))
	for c := range thresholdKeys {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// Rule is one code's entry in the rules file: its severity, its
// thresholds, the advice shown beside it, and the harnesses it is off
// for because their data cannot support it.
type Rule struct {
	Severity      Severity         `json:"severity"`
	Thresholds    map[string]int64 `json:"thresholds"`
	Suggestion    string           `json:"suggestion"`
	Link          string           `json:"link,omitempty"`
	SkipHarnesses []string         `json:"skip_harnesses,omitempty"`
}

// Rules is a parsed rules file.
type Rules struct {
	// Version changes whenever a threshold, severity or suggestion
	// does, so a stored report says which rules made it.
	Version int `json:"version"`
	// PollingTools are tool names that repeat the same call by design,
	// left out of retry_loop.
	PollingTools []string      `json:"polling_tools"`
	Codes        map[Code]Rule `json:"codes"`
}

//go:embed suggestions.json
var suggestionsJSON []byte

// Shipped is the rules this build ships with.
var Shipped = mustParse(suggestionsJSON)

func mustParse(raw []byte) *Rules {
	r, err := Parse(raw)
	if err != nil {
		panic(err)
	}
	return r
}

// Parse reads a rules file and refuses one that does not give every
// code exactly the thresholds it reads, a severity and a suggestion.
func Parse(raw []byte) (*Rules, error) {
	var r Rules
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("diagnose: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("diagnose: data after the object")
	}
	if r.Version < 1 {
		return nil, errors.New("diagnose: no version")
	}
	for c := range r.Codes {
		if _, ok := thresholdKeys[c]; !ok {
			return nil, fmt.Errorf("diagnose: unknown code %q", c)
		}
	}
	for c, keys := range thresholdKeys {
		rule, ok := r.Codes[c]
		if !ok {
			return nil, fmt.Errorf("diagnose: no rule for %s", c)
		}
		if rule.Severity != Info && rule.Severity != Warn && rule.Severity != Error {
			return nil, fmt.Errorf("diagnose: %s: severity %q is not info, warn or error", c, rule.Severity)
		}
		if strings.TrimSpace(rule.Suggestion) == "" {
			return nil, fmt.Errorf("diagnose: %s: no suggestion", c)
		}
		if rule.Link != "" && !strings.HasPrefix(rule.Link, "https://") {
			return nil, fmt.Errorf("diagnose: %s: link %q is not https", c, rule.Link)
		}
		if len(rule.Thresholds) != len(keys) {
			return nil, fmt.Errorf("diagnose: %s: thresholds must be exactly %v", c, keys)
		}
		for _, k := range keys {
			v, ok := rule.Thresholds[k]
			if !ok {
				return nil, fmt.Errorf("diagnose: %s: thresholds must be exactly %v", c, keys)
			}
			if v < 1 {
				return nil, fmt.Errorf("diagnose: %s: threshold %s must be positive", c, k)
			}
		}
	}
	return &r, nil
}

func (r *Rules) t(c Code, key string) int64 { return r.Codes[c].Thresholds[key] }

func (r *Rules) off(c Code, harness string) bool {
	return slices.Contains(r.Codes[c].SkipHarnesses, harness)
}
