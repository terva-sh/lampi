package web

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// The Health panel: profiledoctor's report on a profile's rules, on the
// profile page and in the editor, where its fixes are buttons that
// change the form and preview it. See TKT-01M6EJ43MTF221EJQAQH7K7QT7.

// healthItem is one finding with its rules named for the page.
type healthItem struct {
	profiledoctor.Finding
	RuleText, ByText string
	ReplaceText      []string
	// Edit shows the fix as a button, and Grouped leaves out the message
	// its group already shows.
	Edit, Grouped bool
}

// healthGroup is the findings of one code, in the order the doctor
// listed them. A group of two or more shows as one row with the shared
// message and, in the editor, one button that applies every fix of the
// code (TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2).
type healthGroup struct {
	Code, Level string
	// Message is the message every finding shares, or "".
	Message string
	Items   []healthItem
	// Admits counts the projects the group's rules admit today, Fixes
	// the findings its bulk button applies, and Bulk is that button's
	// label, "" when it offers none.
	Admits, Fixes int
	Bulk          string
}

// healthView is the panel.
type healthView struct {
	profiledoctor.Report
	Items  []healthItem
	Groups []healthGroup
	// DenyTemporary is the deny rules the editor offers for the
	// temporary folders in the findings and the hides a save queues,
	// as text, and DenyRules the rules.
	DenyTemporary []string
	DenyRules     []config.ProjectMatch
	// Safe counts the safe fixes, and Unmatched the rules that admit
	// nothing today, safe or not.
	Safe, Unmatched int
	// Edit shows the fixes as buttons; the profile page links to the
	// editor instead.
	Edit    bool
	EditURL string
}

// diagnose reads name's devices and runs the doctor over p, which may
// be an edit not saved yet.
func (s *Server) diagnose(r *http.Request, name string, p config.Projects) (profiledoctor.Report, error) {
	in, err := profiledoctor.Gather(r.Context(), s.catalog, name, p)
	if err != nil {
		return profiledoctor.Report{}, err
	}
	return profiledoctor.Diagnose(in), nil
}

// pendingHides is hide less the projects p still admits on a device.
// A Remove and hide queues its projects for the save; an edit after it
// that admits them again, such as putting the rule back, takes them off.
func (s *Server) pendingHides(r *http.Request, name string, p config.Projects, hide []string) ([]string, error) {
	if len(hide) == 0 {
		return hide, nil
	}
	in, err := profiledoctor.Gather(r.Context(), s.catalog, name, p)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, h := range hide {
		if k, ok := parseKey(h); ok && !profiledoctor.Admits(in, k) {
			out = append(out, h)
		}
	}
	return out, nil
}

// health is the panel for p, or nil when the devices could not be read:
// a report made without them would call rules safe to remove that are
// in use.
func (s *Server) health(r *http.Request, name string, p config.Projects, edit bool) *healthView {
	rep, err := s.diagnose(r, name, p)
	if err != nil {
		s.logError(r, "reading the profile's devices for its health failed", err)
		return nil
	}
	v := &healthView{Report: rep, Edit: edit, EditURL: profileURL(name) + "/edit"}
	for _, f := range rep.Findings {
		it := healthItem{Finding: f, Edit: edit}
		if f.Rule != nil {
			it.RuleText = ruleText(*f.Rule)
		}
		if f.By != nil {
			it.ByText = ruleText(*f.By)
		}
		for _, r := range f.Replace {
			it.ReplaceText = append(it.ReplaceText, ruleText(r))
		}
		switch f.Fix {
		case profiledoctor.FixSafe:
			v.Safe++
			if f.Code == profiledoctor.CodeTemporary {
				v.Unmatched++
			}
		case profiledoctor.FixRemove:
			v.Unmatched++
		}
		v.Items = append(v.Items, it)
	}
	v.Groups = groupFindings(v.Items, edit)
	return v
}

// bulkFixable reports whether a finding's fix is one the group's bulk
// button applies: a removal or a narrowing of one rule.
func bulkFixable(f profiledoctor.Finding) bool {
	return f.Rule != nil && f.Index >= 0 && (f.Fix == profiledoctor.FixSafe || f.Fix == profiledoctor.FixRemove || f.Fix == profiledoctor.FixNarrow)
}

// bulkFindings is the findings of code the bulk button applies.
func bulkFindings(rep profiledoctor.Report, code string) []profiledoctor.Finding {
	var out []profiledoctor.Finding
	for _, f := range rep.Findings {
		if f.Code == code && bulkFixable(f) {
			out = append(out, f)
		}
	}
	return out
}

// bulkLabel says what applying fs does, as "Remove 15 rules and hide 15
// projects", or "" for fewer than two fixes.
func bulkLabel(fs []profiledoctor.Finding) string {
	if len(fs) < 2 {
		return ""
	}
	var removes, narrows, exact int
	var hide []catalog.ProjectKey
	for _, f := range fs {
		if f.Fix == profiledoctor.FixNarrow && len(f.Replace) > 0 {
			narrows++
			exact += len(f.Replace)
		} else {
			removes++
		}
		for _, h := range f.Hide {
			if !slices.Contains(hide, h) {
				hide = append(hide, h)
			}
		}
	}
	var parts []string
	if removes > 0 {
		parts = append(parts, "remove "+strconv.Itoa(removes)+" "+plural(removes, "rule", "rules"))
	}
	if narrows > 0 {
		parts = append(parts, "narrow "+strconv.Itoa(narrows)+" "+plural(narrows, "rule", "rules")+" to "+strconv.Itoa(exact)+" exact "+plural(exact, "rule", "rules"))
	}
	if len(hide) > 0 {
		parts = append(parts, "hide "+strconv.Itoa(len(hide))+" "+plural(len(hide), "project", "projects"))
	}
	out := parts[0]
	if len(parts) > 1 {
		out = strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
	return strings.ToUpper(out[:1]) + out[1:]
}

// groupFindings gathers items by code, in the order each code first
// appears.
func groupFindings(items []healthItem, edit bool) []healthGroup {
	var out []healthGroup
	at := map[string]int{}
	for _, it := range items {
		i, ok := at[it.Code]
		if !ok {
			i = len(out)
			at[it.Code] = i
			out = append(out, healthGroup{Code: it.Code, Level: it.Level, Message: it.Message})
		}
		g := &out[i]
		if g.Message != it.Message {
			g.Message = ""
		}
		g.Items = append(g.Items, it)
		g.Admits += len(it.Admits)
	}
	for i := range out {
		g := &out[i]
		if len(g.Items) < 2 {
			continue
		}
		var fs []profiledoctor.Finding
		for j := range g.Items {
			g.Items[j].Grouped = g.Message != ""
			if bulkFixable(g.Items[j].Finding) {
				fs = append(fs, g.Items[j].Finding)
			}
		}
		// Apply safe fixes already removes a group of safe fixes.
		allSafe := !slices.ContainsFunc(fs, func(f profiledoctor.Finding) bool { return f.Fix != profiledoctor.FixSafe })
		if edit && !allSafe {
			g.Fixes, g.Bulk = len(fs), bulkLabel(fs)
		}
	}
	return out
}

// temporaryDenies is the deny rules that keep new sessions out of the
// temporary folders rep flags and hide queues, less those deny already
// covers: one per temp root or .cache folder, in the order first seen.
func temporaryDenies(rep profiledoctor.Report, hide []string, deny []config.ProjectMatch) []config.ProjectMatch {
	var dirs []string
	for _, f := range rep.Findings {
		if f.Code == profiledoctor.CodeTemporary && f.Rule != nil && f.Rule.CWDPrefix != "" {
			dirs = append(dirs, f.Rule.CWDPrefix)
		}
	}
	for _, h := range hide {
		if k, ok := parseKey(h); ok && k.Kind == catalog.KeyCWD && profiledoctor.Temporary(k.Key) {
			dirs = append(dirs, k.Key)
		}
	}
	var out []config.ProjectMatch
	for _, d := range dirs {
		r, ok := profiledoctor.TemporaryDeny(d)
		if !ok || slices.Contains(out, r) || slices.ContainsFunc(deny, func(x config.ProjectMatch) bool { return config.Covers(x, r) }) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// offerDeny sets the deny rules the editor offers for the temporary
// folders in the report and in hide, the projects the save hides.
func (v *healthView) offerDeny(hide []string, deny []config.ProjectMatch) {
	if v == nil || !v.Edit {
		return
	}
	v.DenyRules = temporaryDenies(v.Report, hide, deny)
	v.DenyTemporary = nil
	for _, r := range v.DenyRules {
		v.DenyTemporary = append(v.DenyTemporary, ruleText(r))
	}
}
