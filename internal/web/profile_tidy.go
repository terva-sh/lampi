package web

import (
	"net/url"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// The editor's offers to shorten a profile's allow list: remove the
// rules another rule already covers, and replace a run of exact
// git_remote rules under one owner with one git_remote_prefix. Both
// change the form and preview it; nothing is saved until the operator
// saves, after the preview has listed what a wider rule admits.

// coveredRule is an allow rule that another allow rule in the same
// list covers, and the rule that covers it.
type coveredRule struct {
	Rule, By string
}

// coveredRules lists the covered rules in rules for the editor.
func coveredRules(rules []config.ProjectMatch) []coveredRule {
	by := profiledoctor.CoveredBy(rules)
	var out []coveredRule
	for i, r := range rules {
		if j, ok := by[i]; ok {
			out = append(out, coveredRule{Rule: ruleText(r), By: ruleText(rules[j])})
		}
	}
	return out
}

// tidyForm applies the offer the operator pressed, if any, to the rules
// read from the form: "tidy=covered" removes the covered allow rules,
// "fold=OWNER" folds that owner's remotes when ownerFolds offers it
// for those rules, and "folder=DIR" folds the cwd_prefix rules under DIR
// when folderFolds offers it. An empty note is filled
// with what was done, so the saved revision says why the rules went.
func tidyForm(v url.Values, f *profileForm, p *config.Profile) {
	var n int
	var note string
	switch owner, folder := v.Get("fold"), v.Get("folder"); {
	case v.Get("tidy") == "covered":
		p.Projects.Allow, n = profiledoctor.WithoutCovered(p.Projects.Allow)
		note = "Remove " + strconv.Itoa(n) + " covered allow " + plural(n, "rule", "rules")
	case owner != "":
		// Only an owner the editor offers for these rules: a fold is a
		// widening, and the offer is where its threshold lives.
		if !slices.ContainsFunc(profiledoctor.OwnerFolds(p.Projects.Allow), func(f profiledoctor.OwnerFold) bool { return f.Owner == owner }) {
			return
		}
		p.Projects.Allow, n = profiledoctor.FoldOwner(p.Projects.Allow, owner)
		note = "Replace " + strconv.Itoa(n) + " git_remote " + plural(n, "rule", "rules") + " under " + owner + " with one git_remote_prefix"
	case folder != "":
		if !slices.ContainsFunc(profiledoctor.FolderFolds(p.Projects.Allow), func(f profiledoctor.FolderFold) bool { return f.Folder == folder }) {
			return
		}
		p.Projects.Allow, n = profiledoctor.FoldFolder(p.Projects.Allow, folder)
		note = "Replace " + strconv.Itoa(n) + " cwd_prefix " + plural(n, "rule", "rules") + " under " + folder + " with one cwd_prefix"
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
// every repository under its owner. A project with no remote is named by
// its folder, and its owner width is the folder above it, as profiledoctor.ParentOf
// decides.
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

// allowRuleAt is the rule Allow adds for a refused project at width: at
// owner width a git_remote_prefix for the remote's owner, or for a
// project with no remote a cwd_prefix for the folder above it. Otherwise,
// or when the owner would be the bare host or the folder one ParentOf
// refuses, allowRule's.
func allowRuleAt(remote, cwd, width string) (config.ProjectMatch, bool) {
	if o := profiledoctor.OwnerOf(remote); width == widthOwner && o != "" {
		return config.ProjectMatch{GitRemotePrefix: o}, true
	}
	if d := profiledoctor.ParentOf(remote, cwd); width == widthOwner && d != "" {
		return config.ProjectMatch{CWDPrefix: d}, true
	}
	return allowRule(remote, cwd)
}

// withRules is allow with add appended, less every rule of allow that
// one of add covers and is wider than, and the rules it removed. A rule
// of add that a kept rule, or another rule of add, covers is not added,
// so a rule already there stays where it is.
func withRules(allow, add []config.ProjectMatch) (out, removed []config.ProjectMatch) {
	add, _ = profiledoctor.WithoutCovered(add)
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
