package web

import (
	"cmp"
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
//     folder, when the doctor offers that fold;
//   - "fix_all=CODE" applies every removal and narrowing of the code's
//     findings, when the Health panel groups two or more;
//   - "deny_temporary=1" adds the deny rules the panel offers for
//     temporary folders.
//
// Each reads the doctor's report for the form's rules, so it acts only
// on what the Health panel offered for them. An empty note is filled
// with what was done, so the saved revision says why the rules went. A
// fix that cannot be saved is not applied, and the problem returned
// says why.
func (s *Server) tidyForm(r *http.Request, name string, f *profileForm, p *config.Profile) (string, error) {
	v := r.PostForm
	doctor, narrow, owner, folder := v.Get("doctor"), v.Get("narrow"), v.Get("fold"), v.Get("folder")
	fixAll, denyTemp := v.Get("fix_all"), v.Get("deny_temporary") != ""
	if doctor == "" && narrow == "" && owner == "" && folder == "" && fixAll == "" && !denyTemp {
		return "", nil
	}
	rep, err := s.diagnose(r, name, p.Projects, p.Review)
	beforeAllow := slices.Clone(p.Projects.Allow)
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
	case fixAll != "":
		fs := bulkFindings(rep, fixAll)
		if len(fs) < 2 {
			return "", nil
		}
		queued := len(f.Hide)
		var hide []catalog.ProjectKey
		for _, fd := range fs {
			for _, h := range fd.Hide {
				if !slices.Contains(hide, h) {
					hide = append(hide, h)
					if !slices.Contains(f.Hide, h.String()) {
						queued++
					}
				}
			}
		}
		if queued > maxReviewBatch {
			return fmt.Sprintf("Fixing every %s finding would hide %d projects from review, and one save hides at most %d. Nothing was changed. Save the changes so far first, or fix them one at a time.", fixAll, queued, maxReviewBatch), nil
		}
		label := bulkLabel(fs)
		if p.Projects, hide, n = profiledoctor.ApplyFixes(p.Projects, fs); n == 0 {
			return "", nil
		}
		for _, h := range hide {
			if !slices.Contains(f.Hide, h.String()) {
				f.Hide = append(f.Hide, h.String())
			}
		}
		note = label + ": " + fixAll
		if hide != nil {
			note = strings.Replace(note, " project:", " project from review:", 1)
			note = strings.Replace(note, " projects:", " projects from review:", 1)
		}
	case denyTemp:
		add := temporaryDenies(rep, f.Hide, p.Projects.Deny)
		if len(add) == 0 {
			return "", nil
		}
		p.Projects.Deny = append(p.Projects.Deny, add...)
		f.Deny = withBlanks(p.Projects.Deny)
		var texts []string
		for _, r := range add {
			texts = append(texts, ruleText(r))
		}
		if strings.TrimSpace(f.Note) == "" {
			f.Note = "Deny new sessions in temporary folders: " + strings.Join(texts, ", ")
		}
		return "", nil
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
	p.Projects.Allow = preserveReviewAllows(beforeAllow, p.Projects.Allow, p.Review)
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
//
// A rule added goes beside its folder or owner (TKT-01M6ETZV7C0P5F2RDKQPAQCCFW):
// after the last rule listed under the same header, or where the first
// rule it replaces stood, or else at the end. A list grouped by hand or
// by Sort stays grouped, and an order the operator set is kept.
func withRules(allow, add []config.ProjectMatch, visibility ...config.ReviewConfig) (out, removed []config.ProjectMatch) {
	var review config.ReviewConfig
	if len(visibility) > 0 {
		review = visibility[0]
	}
	originalAdd := add
	add, _ = profiledoctor.WithoutCovered(add)
	add = preserveReviewAllows(originalAdd, add, review)
	coveredByAny := func(rules []config.ProjectMatch, r config.ProjectMatch) bool {
		return slices.ContainsFunc(rules, func(a config.ProjectMatch) bool { return config.Covers(a, r) })
	}
	// at[j] is where add[j] replaced its first rule, as an index in out.
	at := make([]int, len(add))
	for j := range at {
		at[j] = -1
	}
	for _, r := range allow {
		replaced := false
		for j, a := range add {
			if config.Covers(a, r) && !config.Covers(r, a) && !review.ProtectsAllow(r) {
				if at[j] < 0 {
					at[j] = len(out)
				}
				replaced = true
			}
		}
		if replaced {
			removed = append(removed, r)
			continue
		}
		out = append(out, r)
	}
	kept := slices.Clone(out)
	// Insert from the end of the list, so an earlier position still
	// names the same place.
	type insert struct {
		pos, j int
		r      config.ProjectMatch
	}
	var ins []insert
	for j, r := range add {
		if coveredByAny(kept, r) && (!review.ProtectsAllow(r) || slices.Contains(kept, r)) {
			continue
		}
		pos := len(kept)
		if at[j] >= 0 {
			pos = at[j]
		}
		if key := groupKey(r); key != "" {
			for i := len(kept) - 1; i >= 0; i-- {
				if groupKey(kept[i]) == key {
					pos = i + 1
					break
				}
			}
		}
		ins = append(ins, insert{pos, j, r})
	}
	// Insert the last place first, so each earlier place still names the
	// same spot; at one place the later rule goes in first, which leaves
	// them in the order add gave.
	slices.SortFunc(ins, func(a, b insert) int { return cmp.Or(b.pos-a.pos, b.j-a.j) })
	for _, in := range ins {
		out = slices.Insert(out, in.pos, in.r)
	}
	return out, removed
}
