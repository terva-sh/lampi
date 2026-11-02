// Package profiledoctor reads a profile's project rules, and the
// projects its devices hold, and says what to change: rules that add
// nothing, rules that reach further than anyone chose, rules for
// folders that will never hold a session again, and groups of rules one
// wider rule would replace. It changes nothing. The dashboard's profile
// editor and serve profiles doctor both read its report; the operator
// saves.
//
// This file holds the rule arithmetic the editor's offers share:
// coverage, owner folds and folder folds.
package profiledoctor

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"terva.sh/lampi/internal/config"
)

// RuleCap bounds the rules a profile holds in each list. The dashboard
// refuses a save past it.
const RuleCap = 500

// MinFold is how many rules under one owner or folder make a fold worth
// offering.
const MinFold = 3

// CoveredBy maps the index of each covered rule in rules to the index
// of a rule that covers it. Of two rules that cover each other, the
// first stays.
func CoveredBy(rules []config.ProjectMatch) map[int]int {
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

// WithoutCovered is rules with every covered rule removed, and how many
// went. Covers is transitive, so what is left still matches every
// project rules matched.
func WithoutCovered(rules []config.ProjectMatch) ([]config.ProjectMatch, int) {
	by := CoveredBy(rules)
	var out []config.ProjectMatch
	for i, r := range rules {
		if _, ok := by[i]; !ok {
			out = append(out, r)
		}
	}
	return out, len(by)
}

// OwnerFold is a run of exact git_remote rules under one owner that one
// git_remote_prefix for the owner would replace.
type OwnerFold struct {
	Owner string `json:"owner"`
	Count int    `json:"count"`
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

// OwnerOf is the owner a remote's owner-wide rule would name, or "" when
// it would be the bare host or the remote folds to nothing.
func OwnerOf(remote string) string {
	o, ok := remoteOwner(config.ProjectMatch{GitRemote: remote})
	if !ok || config.NormalizeRemote(remote) == "" {
		return ""
	}
	return o
}

// OwnerFolds lists the owners with at least MinFold exact remotes in
// rules and no rule that already covers the owner's prefix, most rules
// first.
func OwnerFolds(rules []config.ProjectMatch) []OwnerFold {
	count := map[string]int{}
	for _, r := range rules {
		if o, ok := remoteOwner(r); ok {
			count[o]++
		}
	}
	var out []OwnerFold
	for o, n := range count {
		prefix := config.ProjectMatch{GitRemotePrefix: o}
		if n < MinFold || coveredByAny(rules, prefix) {
			continue
		}
		out = append(out, OwnerFold{Owner: o, Count: n})
	}
	slices.SortFunc(out, func(a, b OwnerFold) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Owner, b.Owner)) })
	return out
}

// FoldOwner replaces the exact remotes under owner in rules with one
// git_remote_prefix, where the first of them stood, and says how many it
// replaced. An owner with no such rule changes nothing.
func FoldOwner(rules []config.ProjectMatch, owner string) ([]config.ProjectMatch, int) {
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

// FolderFold is a group of cwd_prefix rules under one folder that one
// cwd_prefix for the folder would replace.
type FolderFold struct {
	Folder string `json:"folder"`
	Count  int    `json:"count"`
}

// WideFolder reports whether a folder-wide rule may name dir, a clean
// absolute path. A top-level folder such as /tmp, a home directory such
// as /home/me or /Users/me, and the folder that holds home directories
// are refused: a rule for one of them reaches projects nobody chose,
// which is TKT-01M3NQ8324. Those are rules to type on purpose.
func WideFolder(dir string) bool {
	segs := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	if !strings.HasPrefix(dir, "/") || len(segs) < 2 || slices.Contains(segs, "") {
		return false
	}
	return !homes(segs[len(segs)-1]) && !homes(segs[len(segs)-2])
}

// homes reports whether a folder named seg holds home directories.
func homes(seg string) bool { return seg == "home" || seg == "Users" }

// cwdOnly is the clean folder of a rule that sets only an absolute
// cwd_prefix, or false for any other rule.
func cwdOnly(r config.ProjectMatch) (string, bool) {
	c := strings.TrimSpace(r.CWDPrefix)
	if c == "" || r != (config.ProjectMatch{CWDPrefix: r.CWDPrefix}) || !strings.HasPrefix(c, "/") {
		return "", false
	}
	return path.Clean(c), true
}

// ParentOf is the folder a folder-wide Allow names for a project with no
// git remote: the folder above cwd, when WideFolder accepts it, and ""
// otherwise. A project with a remote has none; its owner is OwnerOf's.
func ParentOf(remote, cwd string) string {
	if config.NormalizeRemote(remote) != "" {
		return ""
	}
	c, ok := cwdOnly(config.ProjectMatch{CWDPrefix: cwd})
	if !ok || c == "/" {
		return ""
	}
	if d := path.Dir(c); WideFolder(d) {
		return d
	}
	return ""
}

// FolderFolds lists the folders that hold cwd_prefix rules in at least
// MinFold of their own subfolders, that WideFolder accepts, and that no
// rule already covers, most rules first. Worktrees and scratch trees
// land in such a folder one rule each, and on another device under the
// same layout. A folder and one under it can both be offered: folding
// the deeper one keeps the change narrower.
func FolderFolds(rules []config.ProjectMatch) []FolderFold {
	kids := map[string]map[string]bool{}
	count := map[string]int{}
	for _, r := range rules {
		c, ok := cwdOnly(r)
		if !ok {
			continue
		}
		for child, d := c, path.Dir(c); d != child; child, d = d, path.Dir(d) {
			if !WideFolder(d) {
				continue
			}
			if kids[d] == nil {
				kids[d] = map[string]bool{}
			}
			kids[d][path.Base(child)] = true
			count[d]++
		}
	}
	var out []FolderFold
	for d, k := range kids {
		if len(k) < MinFold || coveredByAny(rules, config.ProjectMatch{CWDPrefix: d}) {
			continue
		}
		out = append(out, FolderFold{Folder: d, Count: count[d]})
	}
	slices.SortFunc(out, func(a, b FolderFold) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Folder, b.Folder))
	})
	return out
}

// FoldFolder replaces the cwd_prefix rules under folder in rules with
// one cwd_prefix for folder, where the first of them stood, and says how
// many it replaced.
func FoldFolder(rules []config.ProjectMatch, folder string) ([]config.ProjectMatch, int) {
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

// coveredByAny reports whether a rule of rules covers r.
func coveredByAny(rules []config.ProjectMatch, r config.ProjectMatch) bool {
	return slices.ContainsFunc(rules, func(a config.ProjectMatch) bool { return config.Covers(a, r) })
}

// RuleText names a rule the way a note or a notice says it: each field
// set, joined with "and", since every one must match.
func RuleText(m config.ProjectMatch) string {
	var parts []string
	for _, f := range []struct{ name, value string }{
		{"git_remote", m.GitRemote},
		{"git_remote_prefix", m.GitRemotePrefix},
		{"cwd_prefix", m.CWDPrefix},
		{"cwd_hash", m.CWDHash},
		{"cwd_glob", m.CWDGlob},
	} {
		if f.value != "" {
			parts = append(parts, f.name+" "+f.value)
		}
	}
	return strings.Join(parts, " and ")
}
