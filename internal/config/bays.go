package config

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// BayRequests is which bays a lake is asked to put a session in
// (TKT-01M3NNF2CE). Each rule whose fields all match names bays; the
// session asks for every bay any matching rule names. A session no rule
// matches asks for Default. The lake decides: it places a session only
// in the bays this device may write, and applies its own rules
// (docs/policy.md#routing).
type BayRequests struct {
	Rules   []BayRequestRule `json:"rules,omitempty"`
	Default []string         `json:"default,omitempty"`
}

// BayRequestRule is one rule. Match is read the way an allow rule is,
// exactly: cwd_prefix covers the folder and everything under it.
// Harness, when set, must equal the session's harness. A rule with
// neither matches nothing and is refused.
type BayRequestRule struct {
	ProjectMatch
	Harness string   `json:"harness,omitempty"`
	Bays    []string `json:"bays"`
}

// bayRefPattern is a bay name or a bay id as the lake prints them.
var bayRefPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

// Validate refuses a rule that matches nothing, names no bay, or names
// a bay no lake could have.
func (b BayRequests) Validate() error {
	for i, r := range b.Rules {
		at := "bays.rules[" + strconv.Itoa(i) + "]"
		if r.ProjectMatch.empty() && r.Harness == "" {
			return fmt.Errorf("%s: set a project field or a harness", at)
		}
		if err := (Projects{Allow: []ProjectMatch{r.ProjectMatch}}).Validate(); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		if len(r.Bays) == 0 {
			return fmt.Errorf("%s: bays is empty", at)
		}
		if err := checkBayRefs(at+".bays", r.Bays); err != nil {
			return err
		}
	}
	return checkBayRefs("bays.default", b.Default)
}

func checkBayRefs(at string, refs []string) error {
	for _, ref := range refs {
		if !bayRefPattern.MatchString(ref) {
			return fmt.Errorf("%s: %q is not a bay name", at, ref)
		}
	}
	return nil
}

// BayChoice is the bays a session asks for, and why: the index of each
// rule that matched, or none when the default applied.
type BayChoice struct {
	Bays    []string
	Rules   []int
	Default bool
}

// For is the bays a session of harness in id asks for. Bays are in the
// order the rules name them, each once.
func (b BayRequests) For(harness string, id ProjectID) BayChoice {
	var c BayChoice
	for i, r := range b.Rules {
		if r.Harness != "" && r.Harness != harness {
			continue
		}
		if !r.ProjectMatch.empty() && !r.ProjectMatch.matches(id) {
			continue
		}
		c.Rules = append(c.Rules, i)
		for _, bay := range r.Bays {
			if !slices.Contains(c.Bays, bay) {
				c.Bays = append(c.Bays, bay)
			}
		}
	}
	if len(c.Rules) == 0 && len(b.Default) > 0 {
		c.Default = true
		for _, bay := range b.Default {
			if !slices.Contains(c.Bays, bay) {
				c.Bays = append(c.Bays, bay)
			}
		}
	}
	return c
}
