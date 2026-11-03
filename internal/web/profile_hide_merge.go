package web

import (
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/config"
)

// mergeHideForm consolidates only the ticked folder allow rules. Unticked
// narrower rules retain their explicit exception, as do review show rules.
func (s *Server) mergeHideForm(r *http.Request, name string, f *profileForm, p *config.Profile) (*mergeView, error) {
	v := mergeView{Kind: "cwd_prefix", HideRule: true, Before: len(p.Projects.Allow)}
	var values []string
	for _, rule := range f.Picked {
		v.Picked = append(v.Picked, ruleText(rule))
		kind, value := mergeKind(rule)
		if kind != "cwd_prefix" {
			v.Problem = "Hide a folder prefix by ticking only folder allow rules with one cwd_prefix condition."
			return &v, nil
		}
		values = append(values, value)
	}
	if len(values) < 2 {
		v.Problem = "Tick two or more folder allow rules to replace with a hide prefix."
		return &v, nil
	}
	v.Prefix = strings.TrimSpace(r.PostForm.Get("merge_prefix"))
	if v.Prefix == "" {
		v.Prefix = "/" + commonPrefix(values)
	}
	v.Prefix = path.Clean(v.Prefix)
	rule := config.ReviewRule{CWDPrefix: v.Prefix, Action: "hide"}
	if err := (config.ReviewConfig{Rules: []config.ReviewRule{rule}}).Validate(); err != nil {
		v.Problem = err.Error()
		return &v, nil
	}
	next := *p
	next.Projects.Allow = nil
	prefix := config.ProjectMatch{CWDPrefix: v.Prefix}
	for _, allow := range p.Projects.Allow {
		if slices.Contains(f.Picked, allow) && config.Covers(prefix, allow) {
			v.Replaces = append(v.Replaces, ruleText(allow))
			continue
		}
		next.Projects.Allow = append(next.Projects.Allow, allow)
	}
	for _, picked := range f.Picked {
		if !config.Covers(prefix, picked) {
			v.Missed = append(v.Missed, ruleText(picked))
		}
	}
	next.Review.Rules = nil
	for _, existing := range p.Review.Rules {
		if existing.CWDPrefix != v.Prefix {
			next.Review.Rules = append(next.Review.Rules, existing)
		}
	}
	next.Review.Rules = append(next.Review.Rules, rule)
	if err := next.Validate(); err != nil {
		v.Problem = err.Error()
		return &v, nil
	}
	v.RuleText, v.After = "hide cwd_prefix "+v.Prefix, len(next.Projects.Allow)
	users, err := s.profileUsers(r.Context())
	if err != nil {
		return nil, err
	}
	visibility, err := s.reviewVisibilityReach(r.Context(), *p, next, users[name])
	if err != nil {
		return nil, err
	}
	v.Visibility = &visibility
	if r.PostForm.Get("merge_apply") != "hide" {
		return &v, nil
	}
	*p = next
	f.Allow, f.Review, f.Picked = withBlanks(next.Projects.Allow), reviewBlanks(next.Review.Rules), nil
	if f.Note == "" {
		f.Note = "Replace " + strconv.Itoa(len(v.Replaces)) + " selected allow rules with " + v.RuleText
	}
	return nil, nil
}
