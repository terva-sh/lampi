package web

import (
	"cmp"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/config"
)

// The editor's offers to shorten a profile's allow list: remove the
// rules another rule already covers, and replace a run of exact
// git_remote rules under one owner with one git_remote_prefix. Both
// change the form and preview it; nothing is saved until the operator
// saves, after the preview has listed what a wider rule admits.

// minFold is how many exact remotes under one owner make a fold worth
// offering.
const minFold = 3

// coveredRule is an allow rule that another allow rule in the same
// list covers, and the rule that covers it.
type coveredRule struct {
	Rule, By string
}

// ownerFold is a run of exact git_remote rules under one owner that one
// git_remote_prefix for the owner would replace.
type ownerFold struct {
	Owner string
	Count int
}

// coveredBy maps the index of each covered rule in rules to the index
// of a rule that covers it. Of two rules that cover each other, the
// first stays.
func coveredBy(rules []config.ProjectMatch) map[int]int {
	out := map[int]int{}
	for i, b := range rules {
		for j, a := range rules {
			if i != j && config.Covers(a, b) && (j < i || !config.Covers(b, a)) {
				out[i] = j
				break
			}
		}
	}
	return out
}

// coveredRules lists the covered rules in rules for the editor.
func coveredRules(rules []config.ProjectMatch) []coveredRule {
	by := coveredBy(rules)
	var out []coveredRule
	for i, r := range rules {
		if j, ok := by[i]; ok {
			out = append(out, coveredRule{Rule: ruleText(r), By: ruleText(rules[j])})
		}
	}
	return out
}

// withoutCovered is rules with every covered rule removed, and how many
// went. Covers is transitive, so what is left still matches every
// project rules matched.
func withoutCovered(rules []config.ProjectMatch) ([]config.ProjectMatch, int) {
	by := coveredBy(rules)
	var out []config.ProjectMatch
	for i, r := range rules {
		if _, ok := by[i]; !ok {
			out = append(out, r)
		}
	}
	return out, len(by)
}

// remoteOwner is the owner of an exact git_remote rule: its folded
// remote less the last segment. A rule with any other field set, or
// whose owner would be the bare host, has none: a host-wide rule is one
// to type on purpose.
func remoteOwner(r config.ProjectMatch) (string, bool) {
	if r.GitRemote == "" || r != (config.ProjectMatch{GitRemote: r.GitRemote}) {
		return "", false
	}
	owner := path.Dir(config.NormalizeRemote(r.GitRemote))
	return owner, strings.Contains(owner, "/")
}

// ownerFolds lists the owners with at least minFold exact remotes in
// rules and no rule that already covers the owner's prefix, most rules
// first.
func ownerFolds(rules []config.ProjectMatch) []ownerFold {
	count := map[string]int{}
	for _, r := range rules {
		if o, ok := remoteOwner(r); ok {
			count[o]++
		}
	}
	var out []ownerFold
	for o, n := range count {
		prefix := config.ProjectMatch{GitRemotePrefix: o}
		if n < minFold || slices.ContainsFunc(rules, func(r config.ProjectMatch) bool { return config.Covers(r, prefix) }) {
			continue
		}
		out = append(out, ownerFold{Owner: o, Count: n})
	}
	slices.SortFunc(out, func(a, b ownerFold) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Owner, b.Owner)) })
	return out
}

// foldOwner replaces the exact remotes under owner in rules with one
// git_remote_prefix, where the first of them stood, and says how many it
// replaced. An owner with no such rule changes nothing.
func foldOwner(rules []config.ProjectMatch, owner string) ([]config.ProjectMatch, int) {
	var out []config.ProjectMatch
	n := 0
	for _, r := range rules {
		if o, ok := remoteOwner(r); ok && o == owner {
			if n == 0 {
				out = append(out, config.ProjectMatch{GitRemotePrefix: owner})
			}
			n++
			continue
		}
		out = append(out, r)
	}
	return out, n
}

// tidyForm applies the offer the operator pressed, if any, to the rules
// read from the form: "tidy=covered" removes the covered allow rules,
// and "fold=OWNER" folds that owner's remotes. An empty note is filled
// with what was done, so the saved revision says why the rules went.
func tidyForm(v url.Values, f *profileForm, p *config.Profile) {
	var n int
	var note string
	switch owner := v.Get("fold"); {
	case v.Get("tidy") == "covered":
		p.Projects.Allow, n = withoutCovered(p.Projects.Allow)
		note = "Remove " + strconv.Itoa(n) + " covered allow " + plural(n, "rule", "rules")
	case owner != "":
		p.Projects.Allow, n = foldOwner(p.Projects.Allow, owner)
		note = "Replace " + strconv.Itoa(n) + " git_remote " + plural(n, "rule", "rules") + " under " + owner + " with one git_remote_prefix"
	}
	if n == 0 {
		return
	}
	f.Allow = withBlanks(p.Projects.Allow)
	if strings.TrimSpace(f.Note) == "" {
		f.Note = note
	}
}

// The widths Allow can add a rule at: the project's own repository, or
// every repository under its owner.
const (
	widthRepository = "repository"
	widthOwner      = "owner"
)

// readWidth is the width a form or request names; empty is the
// repository.
func readWidth(s string) (string, bool) {
	switch s {
	case "", widthRepository:
		return widthRepository, true
	case widthOwner:
		return widthOwner, true
	}
	return "", false
}

// ownerOf is the owner a remote's owner-wide rule would name, or "" when
// it would be the bare host or the remote folds to nothing.
func ownerOf(remote string) string {
	o, ok := remoteOwner(config.ProjectMatch{GitRemote: remote})
	if !ok || config.NormalizeRemote(remote) == "" {
		return ""
	}
	return o
}

// allowRuleAt is the rule Allow adds for a refused project at width: at
// owner width a git_remote_prefix for the remote's owner, and otherwise,
// or when the owner would be the bare host, allowRule's.
func allowRuleAt(remote, cwd, width string) (config.ProjectMatch, bool) {
	if o := ownerOf(remote); width == widthOwner && o != "" {
		return config.ProjectMatch{GitRemotePrefix: o}, true
	}
	return allowRule(remote, cwd)
}

// withRules is allow with add appended, less every rule of allow that
// one of add covers and is wider than, and the rules it removed. A rule
// of add that a kept rule, or another rule of add, covers is not added,
// so a rule already there stays where it is.
func withRules(allow, add []config.ProjectMatch) (out, removed []config.ProjectMatch) {
	add, _ = withoutCovered(add)
	coveredByAny := func(rules []config.ProjectMatch, r config.ProjectMatch) bool {
		return slices.ContainsFunc(rules, func(a config.ProjectMatch) bool { return config.Covers(a, r) })
	}
	for _, r := range allow {
		if slices.ContainsFunc(add, func(a config.ProjectMatch) bool { return config.Covers(a, r) && !config.Covers(r, a) }) {
			removed = append(removed, r)
			continue
		}
		out = append(out, r)
	}
	kept := slices.Clone(out)
	for _, r := range add {
		if !coveredByAny(kept, r) {
			out = append(out, r)
		}
	}
	return out, removed
}
