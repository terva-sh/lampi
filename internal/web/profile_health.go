package web

import (
	"net/http"

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
}

// healthView is the panel.
type healthView struct {
	profiledoctor.Report
	Items []healthItem
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
		it := healthItem{Finding: f}
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
	return v
}
