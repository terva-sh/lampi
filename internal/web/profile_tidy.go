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

// folderFold is a group of cwd_prefix rules under one folder that one
// cwd_prefix for the folder would replace.
type folderFold struct {
	Folder string
	Count  int
}

// wideFolder reports whether a folder-wide rule may name dir, a clean
// absolute path. A top-level folder such as /tmp, a home directory such
// as /home/me or /Users/me, and the folder that holds home directories
// are refused, as are root's home directories: a rule for one of them reaches projects nobody chose,
// which is TKT-01M3NQ8324. Those are rules to type on purpose.
func wideFolder(dir string) bool {
	segs := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	if !strings.HasPrefix(dir, "/") || len(segs) < 2 || slices.Contains(segs, "") {
		return false
	}
	homes := func(s string) bool { return s == "home" || s == "Users" }
	return !homes(segs[len(segs)-1]) && !homes(segs[len(segs)-2]) && !slices.Contains(rootHomes, dir)
}

// rootHomes are the home directories of root, which sit outside the
// home and Users folders: /root on Linux, /var/root on macOS.
var rootHomes = []string{"/root", "/var/root"}

// cwdOnly is the clean folder of a rule that sets only an absolute
// cwd_prefix, or false for any other rule.
func cwdOnly(r config.ProjectMatch) (string, bool) {
	c := strings.TrimSpace(r.CWDPrefix)
	if c == "" || r != (config.ProjectMatch{CWDPrefix: r.CWDPrefix}) || !strings.HasPrefix(c, "/") {
		return "", false
	}
	return path.Clean(c), true
}

// parentOf is the folder a folder-wide Allow names for a project with no
// git remote: the folder above cwd, when wideFolder accepts it, and ""
// otherwise. A project with a remote has none; its owner is ownerOf's.
func parentOf(remote, cwd string) string {
	if config.NormalizeRemote(remote) != "" {
		return ""
	}
	c, ok := cwdOnly(config.ProjectMatch{CWDPrefix: cwd})
	if !ok || c == "/" {
		return ""
	}
	if d := path.Dir(c); wideFolder(d) {
		return d
	}
	return ""
}

// folderFolds lists the folders that hold cwd_prefix rules in at least
// minFold of their own subfolders, that wideFolder accepts, and that no
// rule already covers, most rules first. Worktrees and scratch trees
// land in such a folder one rule each, and on another device under the
// same layout. A folder and one under it can both be offered: folding
// the deeper one keeps the change narrower.
func folderFolds(rules []config.ProjectMatch) []folderFold {
	kids := map[string]map[string]bool{}
	count := map[string]int{}
	for _, r := range rules {
		c, ok := cwdOnly(r)
		if !ok {
			continue
		}
		for child, d := c, path.Dir(c); d != child; child, d = d, path.Dir(d) {
			if !wideFolder(d) {
				continue
			}
			if kids[d] == nil {
				kids[d] = map[string]bool{}
			}
			kids[d][path.Base(child)] = true
			count[d]++
		}
	}
	var out []folderFold
	for d, k := range kids {
		prefix := config.ProjectMatch{CWDPrefix: d}
		if len(k) < minFold || slices.ContainsFunc(rules, func(r config.ProjectMatch) bool { return config.Covers(r, prefix) }) {
			continue
		}
		out = append(out, folderFold{Folder: d, Count: count[d]})
	}
	slices.SortFunc(out, func(a, b folderFold) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Folder, b.Folder))
	})
	return out
}

// foldFolder replaces the cwd_prefix rules under folder in rules with
// one cwd_prefix for folder, where the first of them stood, and says how
// many it replaced.
func foldFolder(rules []config.ProjectMatch, folder string) ([]config.ProjectMatch, int) {
	var out []config.ProjectMatch
	wide := config.ProjectMatch{CWDPrefix: folder}
	n := 0
	for _, r := range rules {
		if c, ok := cwdOnly(r); ok && c != folder && config.Covers(wide, r) {
			if n == 0 {
				out = append(out, wide)
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
// "fold=OWNER" folds that owner's remotes when ownerFolds offers it
// for those rules, and "folder=DIR" folds the cwd_prefix rules under DIR
// when folderFolds offers it. An empty note is filled
// with what was done, so the saved revision says why the rules went.
func tidyForm(v url.Values, f *profileForm, p *config.Profile) {
	var n int
	var note string
	switch owner, folder := v.Get("fold"), v.Get("folder"); {
	case v.Get("tidy") == "covered":
		p.Projects.Allow, n = withoutCovered(p.Projects.Allow)
		note = "Remove " + strconv.Itoa(n) + " covered allow " + plural(n, "rule", "rules")
	case owner != "":
		// Only an owner the editor offers for these rules: a fold is a
		// widening, and the offer is where its threshold lives.
		if !slices.ContainsFunc(ownerFolds(p.Projects.Allow), func(f ownerFold) bool { return f.Owner == owner }) {
			return
		}
		p.Projects.Allow, n = foldOwner(p.Projects.Allow, owner)
		note = "Replace " + strconv.Itoa(n) + " git_remote " + plural(n, "rule", "rules") + " under " + owner + " with one git_remote_prefix"
	case folder != "":
		if !slices.ContainsFunc(folderFolds(p.Projects.Allow), func(f folderFold) bool { return f.Folder == folder }) {
			return
		}
		p.Projects.Allow, n = foldFolder(p.Projects.Allow, folder)
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
// its folder, and its owner width is the folder above it, as parentOf
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
// owner width a git_remote_prefix for the remote's owner, or for a
// project with no remote a cwd_prefix for the folder above it. Otherwise,
// or when the owner would be the bare host or the folder one parentOf
// refuses, allowRule's.
func allowRuleAt(remote, cwd, width string) (config.ProjectMatch, bool) {
	if o := ownerOf(remote); width == widthOwner && o != "" {
		return config.ProjectMatch{GitRemotePrefix: o}, true
	}
	if d := parentOf(remote, cwd); width == widthOwner && d != "" {
		return config.ProjectMatch{CWDPrefix: d}, true
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
