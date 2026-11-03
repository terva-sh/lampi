package config

import (
	"fmt"
	"path"
	"strings"
)

// ReviewConfig controls lake-side review visibility. It never changes capture
// permission and is omitted from the profile delivered to devices.
type ReviewConfig struct {
	Rules []ReviewRule `json:"rules,omitempty"`
}

// ReviewRule hides or shows a directory tree. The deepest matching prefix
// wins; hide wins a tie, regardless of list order.
type ReviewRule struct {
	CWDPrefix string `json:"cwd_prefix"`
	Action    string `json:"action"`
}

func (r ReviewConfig) Validate() error {
	if len(r.Rules) > 500 {
		return fmt.Errorf("review.rules: at most 500 rules")
	}
	for i, rule := range r.Rules {
		if !strings.HasPrefix(rule.CWDPrefix, "/") || strings.ContainsAny(rule.CWDPrefix, "\x00\r\n") || len(rule.CWDPrefix) > 4096 || path.Clean(rule.CWDPrefix) != rule.CWDPrefix {
			return fmt.Errorf("review.rules[%d].cwd_prefix: use a clean absolute directory", i)
		}
		if rule.Action != "hide" && rule.Action != "show" {
			return fmt.Errorf("review.rules[%d].action: choose hide or show", i)
		}
	}
	return nil
}

// HiddenBy evaluates inventoried paths lexically, without resolving them on
// the lake host. A matching allow rule is an exception when it is provably
// narrower than the winning hide rule. An exact cwd_hash also names one
// checkout. Remote-only rules do not establish a narrower directory tree.
func (r ReviewConfig) HiddenBy(id ProjectID, allow []ProjectMatch) (ReviewRule, bool) {
	var winner ReviewRule
	for _, rule := range r.Rules {
		if !(ProjectMatch{CWDPrefix: rule.CWDPrefix}).Matches(id) {
			continue
		}
		if len(rule.CWDPrefix) > len(winner.CWDPrefix) || rule.CWDPrefix == winner.CWDPrefix && rule.Action == "hide" {
			winner = rule
		}
	}
	if winner.Action != "hide" {
		return ReviewRule{}, false
	}
	broad := ProjectMatch{CWDPrefix: winner.CWDPrefix}
	for _, rule := range allow {
		if rule.Matches(id) && (rule.CWDHash != "" || Covers(broad, rule) && !Covers(rule, broad)) {
			return ReviewRule{}, false
		}
	}
	return winner, true
}

// ProtectsAllow reports rules whose removal could hide an exception, even
// when a broader allow rule already permits the same uploads.
func (r ReviewConfig) ProtectsAllow(rule ProjectMatch) bool {
	for _, visibility := range r.Rules {
		broad := ProjectMatch{CWDPrefix: visibility.CWDPrefix}
		if visibility.Action == "hide" && (rule.CWDHash != "" || Covers(broad, rule) && !Covers(rule, broad)) {
			return true
		}
	}
	return false
}

// DeviceProfile is the capture configuration understood by existing agents.
// Review visibility is lake-only and does not change its content version.
func (p Profile) DeviceProfile() Profile {
	p.Review = ReviewConfig{}
	return p
}
