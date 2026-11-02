package web

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// The editor's fixes, which the Health panel offers from the profile
// doctor's report: remove the rules that change nothing, narrow a rule
// that reaches too far, and fold a group of rules into one wider rule.
// Each changes the form and previews it; nothing is saved until the
// operator saves, after the preview has listed what the change admits
// and drops.

// tidyForm applies the fix the operator pressed, if any, to the rules
// read from the form, for profile name:
//
//   - "doctor=safe" removes every rule the doctor's safe fixes name;
//   - "doctor=unmatched" removes every rule that admits nothing today;
//   - "narrow=INDEX" applies the narrowing fix for the allow rule at
//     INDEX, and adds the projects it hides to the form;
//   - "fold=OWNER" and "folder=DIR" fold the rules under an owner or a
//     folder, when the doctor offers that fold.
//
// Each reads the doctor's report for the form's rules, so it acts only
// on what the Health panel offered for them. An empty note is filled
// with what was done, so the saved revision says why the rules went. A
// fix that cannot be saved is not applied, and the problem returned
// says why.
func (s *Server) tidyForm(r *http.Request, name string, f *profileForm, p *config.Profile) (string, error) {
	v := r.PostForm
	doctor, narrow, owner, folder := v.Get("doctor"), v.Get("narrow"), v.Get("fold"), v.Get("folder")
	if doctor == "" && narrow == "" && owner == "" && folder == "" {
		return "", nil
	}
	rep, err := s.diagnose(r, name, p.Projects)
	if err != nil {
		return "", err
	}
	offered := func(code, group string) bool {
		return slices.ContainsFunc(rep.Findings, func(f profiledoctor.Finding) bool { return f.Code == code && f.Group == group })
	}
	var n int
	var note string
	switch {
	case doctor == "safe":
		p.Projects, n = profiledoctor.ApplySafe(p.Projects, rep)
		note = "Remove " + strconv.Itoa(n) + " allow " + plural(n, "rule that changes", "rules that change") + " nothing"
	case doctor == "unmatched":
		p.Projects, n = profiledoctor.ApplyUnmatched(p.Projects, rep)
		note = "Remove " + strconv.Itoa(n) + " allow " + plural(n, "rule", "rules") + " no project matches"
	case narrow != "":
		i, err := strconv.Atoi(narrow)
		k := slices.IndexFunc(rep.Findings, func(f profiledoctor.Finding) bool {
			return err == nil && f.Index == i && f.Fix == profiledoctor.FixNarrow
		})
		if k < 0 {
			return "", nil
		}
		fd := rep.Findings[k]
		// One save hides at most maxReviewBatch projects, as the review
		// queue does: a fix past that could be previewed but not saved.
		queued := len(f.Hide)
		for _, h := range fd.Hide {
			if !slices.Contains(f.Hide, h.String()) {
				queued++
			}
		}
		if queued > maxReviewBatch {
			return fmt.Sprintf("Removing %s would hide %d projects from review, and one save hides at most %d. Nothing was changed. Save the changes so far first, or hide its projects from the review queue and then remove the rule.", ruleText(*fd.Rule), queued, maxReviewBatch), nil
		}
		var hide []catalog.ProjectKey
		var ok bool
		if p.Projects, hide, ok = profiledoctor.ApplyNarrow(p.Projects, fd); !ok {
			return "", nil
		}
		n = 1
		for _, h := range hide {
			if !slices.Contains(f.Hide, h.String()) {
				f.Hide = append(f.Hide, h.String())
			}
		}
		switch {
		case len(hide) > 0:
			note = "Remove temporary rule " + ruleText(*fd.Rule) + " and hide " + strconv.Itoa(len(hide)) + " " + plural(len(hide), "project", "projects") + " from review"
		case len(fd.Replace) == 0:
			note = "Remove " + ruleText(*fd.Rule)
		default:
			note = "Narrow " + ruleText(*fd.Rule) + " to " + strconv.Itoa(len(fd.Replace)) + " exact " + plural(len(fd.Replace), "rule", "rules")
		}
	case owner != "":
		// Only a fold the panel offers for these rules: a fold is a
		// widening, and the offer is where its threshold lives.
		if !offered(profiledoctor.CodeOwnerFold, owner) {
			return "", nil
		}
		p.Projects.Allow, n = profiledoctor.FoldOwner(p.Projects.Allow, owner)
		note = "Replace " + strconv.Itoa(n) + " git_remote " + plural(n, "rule", "rules") + " under " + owner + " with one git_remote_prefix"
	case folder != "":
		if !offered(profiledoctor.CodeFolderFold, folder) {
			return "", nil
		}
		p.Projects.Allow, n = profiledoctor.FoldFolder(p.Projects.Allow, folder)
		note = "Replace " + strconv.Itoa(n) + " cwd_prefix " + plural(n, "rule", "rules") + " under " + folder + " with one cwd_prefix"
	}
	if n == 0 {
		return "", nil
	}
	f.Allow = withBlanks(p.Projects.Allow)
	if strings.TrimSpace(f.Note) == "" {
		f.Note = note
	}
	return "", nil
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
	if g, ok := profiledoctor.GroupOf(remote, cwd); width == widthOwner && ok {
		return g, true
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
