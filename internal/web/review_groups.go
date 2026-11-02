package web

import (
	"cmp"
	"net/http"
	"slices"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// Groups on the review queue: the projects that one Allow at owner
// width would take as one rule, and the projects in temporary folders,
// each shown as one row with one action for all of them. A group's
// actions name the group, and the lake reads its projects from the
// queue as it is when the form arrives, so a group larger than one batch
// works. See TKT-01M6ETFR3B.

const (
	groupOwner     = "owner"
	groupFolder    = "folder"
	groupTemporary = "temporary"
	// maxGroupKeys bounds the projects one group action hides or
	// allows, well past any queue the lake has seen.
	maxGroupKeys = 20000
)

// reviewGroup is one group of the projects that need review.
type reviewGroup struct {
	// ID names the group in a form: its kind, a space, and the owner or
	// folder; the temporary group is its kind alone.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Rule is the allow rule that takes the whole group, absent for the
	// temporary group, which is hidden rather than allowed.
	Rule     *config.ProjectMatch `json:"rule,omitempty"`
	RuleText string               `json:"-"`
	Keys     []catalog.ProjectKey `json:"keys"`
	Sessions int                  `json:"sessions"`
	Bytes    int64                `json:"bytes"`
	Newest   time.Time            `json:"newest,omitzero"`
	Rows     []reviewRow          `json:"-"`
}

// temporaryRow reports whether every copy of row sits in a temporary
// folder, so no new session will start there.
func temporaryRow(row reviewRow) bool {
	for _, sg := range row.Sightings {
		if sg.Project.CWD == "" || !profiledoctor.Temporary(sg.Project.CWD) {
			return false
		}
	}
	return len(row.Sightings) > 0
}

// groupOfRow is the group an owner-width Allow puts row in, if any.
func groupOfRow(row reviewRow) (reviewGroup, bool) {
	remote, cwd := "", row.Key.Key
	if row.Key.Kind == catalog.KeyGitRemote {
		remote, cwd = row.Key.Key, ""
	}
	m, ok := profiledoctor.GroupOf(remote, cwd)
	if !ok {
		return reviewGroup{}, false
	}
	g := reviewGroup{Kind: groupFolder, ID: groupFolder + " " + m.CWDPrefix, Rule: &m, RuleText: ruleText(m)}
	if m.GitRemotePrefix != "" {
		g.Kind, g.ID = groupOwner, groupOwner+" "+m.GitRemotePrefix
	}
	return g, true
}

// groupNeeds splits rows into groups of at least MinFold projects, most
// projects first, and the rows no group holds, in their order. Rows in
// temporary folders group apart from the rest, and are marked.
func groupNeeds(rows []reviewRow) ([]reviewGroup, []reviewRow) {
	byID := map[string]*reviewGroup{}
	var order []string
	place := map[int]string{}
	for i := range rows {
		var g reviewGroup
		if rows[i].Temporary = temporaryRow(rows[i]); rows[i].Temporary {
			g = reviewGroup{ID: groupTemporary, Kind: groupTemporary}
		} else if gg, ok := groupOfRow(rows[i]); ok {
			g = gg
		} else {
			continue
		}
		if byID[g.ID] == nil {
			byID[g.ID] = &g
			order = append(order, g.ID)
		}
		place[i] = g.ID
	}
	for i, row := range rows {
		if id, ok := place[i]; ok {
			g := byID[id]
			g.Rows = append(g.Rows, row)
		}
	}
	groups := []reviewGroup{}
	singles := []reviewRow{}
	for _, id := range order {
		g := byID[id]
		if len(g.Rows) < profiledoctor.MinFold {
			continue
		}
		for _, row := range g.Rows {
			g.Keys = append(g.Keys, row.Key)
			g.Sessions += row.Sessions
			g.Bytes += row.Bytes
			if row.Newest.After(g.Newest) {
				g.Newest = row.Newest
			}
		}
		groups = append(groups, *g)
	}
	for i, row := range rows {
		if id, ok := place[i]; !ok || len(byID[id].Rows) < profiledoctor.MinFold {
			singles = append(singles, row)
		}
	}
	slices.SortStableFunc(groups, func(a, b reviewGroup) int {
		return cmp.Or(cmp.Compare(len(b.Rows), len(a.Rows)), cmp.Compare(a.ID, b.ID))
	})
	return groups, singles
}

// groupFrom reads the group id names from the queue as it is now, under
// the filter of the page the form came from.
func (s *Server) groupFrom(r *http.Request, id string, f reviewFilter) (reviewGroup, bool, error) {
	ctx, cancel := readContext(r)
	defer cancel()
	q, err := s.readReview(ctx, f, s.now())
	if err != nil {
		return reviewGroup{}, false, err
	}
	i := slices.IndexFunc(q.Groups, func(g reviewGroup) bool { return g.ID == id })
	if i < 0 || len(q.Groups[i].Keys) > maxGroupKeys {
		return reviewGroup{}, false, nil
	}
	return q.Groups[i], true, nil
}

// groupGone says what to do when a form names a group the queue no
// longer holds.
const groupGone = "That group is no longer on the queue as it was: its projects were allowed, hidden or decided since, or fewer than three are left. Reload to see the queue as it is now."

// reviewSelection reads the projects an Allow form names, by group or by
// key, and the width to allow them at. It answers the request itself and
// returns false when the form names none.
func (s *Server) reviewSelection(w http.ResponseWriter, r *http.Request, back string) (keys []catalog.ProjectKey, width, group string, ok bool) {
	if group = r.PostForm.Get("group"); group != "" {
		g, found, err := s.groupFrom(r, group, filterOf(back))
		switch {
		case err != nil:
			pageError(w, r, err)
		case !found:
			s.renderBack(w, r, back, groupGone, http.StatusConflict)
		case g.Rule == nil:
			s.renderBack(w, r, back, "Projects in temporary folders are hidden, not allowed: no new session starts there.", http.StatusBadRequest)
		default:
			return g.Keys, widthOwner, group, true
		}
		return nil, "", "", false
	}
	if keys, ok = readKeys(r.PostForm["key"]); !ok {
		s.renderBack(w, r, back, "Choose at least one project to allow, and no more than 500 at once.", http.StatusBadRequest)
		return nil, "", "", false
	}
	if width, ok = readWidth(r.PostForm.Get("width")); !ok {
		s.renderBack(w, r, back, "Allow each repository, or each repository's owner.", http.StatusBadRequest)
		return nil, "", "", false
	}
	return keys, width, "", true
}

// groupNote is the note a group's Hide all records when the form gives
// none.
func groupNote(g reviewGroup) string {
	switch g.Kind {
	case groupTemporary:
		return "Hidden with the temporary folders group"
	case groupOwner:
		return truncate("Hidden with the group under "+g.Rule.GitRemotePrefix, catalog.MaxHideNote)
	}
	return truncate("Hidden with the group under "+g.Rule.CWDPrefix, catalog.MaxHideNote)
}

// reviewGroupView is one group's block on the review page.
type reviewGroupView struct {
	View  reviewView
	Group reviewGroup
}
