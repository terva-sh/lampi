package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// The Allow action on a device's page: an allow rule for a project the
// device refused, added to the profile the device uses. It saves
// nothing itself. It opens the profile editor with the rule added and
// previewed, showing every device the profile reaches, and the operator
// saves from there, with a note if they want one, against the revision
// the preview read.

// allowable reports whether an allow rule can let a refused project
// through: a deny rule wins over any allow rule, and a session with no
// cwd matches none.
func allowable(reason string) bool {
	return reason != config.RefusedByDeny && reason != config.RefusedNoCWD
}

// allowRule is the rule for a refused project: its folded git remote
// when it has one, since that names the repository in every checkout,
// and otherwise its cwd.
func allowRule(remote, cwd string) (config.ProjectMatch, bool) {
	rules := normalizeRules([]config.ProjectMatch{{GitRemote: remote}})
	if len(rules) == 0 {
		rules = normalizeRules([]config.ProjectMatch{{CWDPrefix: cwd}})
	}
	if len(rules) == 0 {
		return config.ProjectMatch{}, false
	}
	return rules[0], true
}

func (s *Server) deviceAllowPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	id := r.PathValue("id")
	d, err := deviceByID(r.Context(), s.catalog, id)
	switch {
	case errors.Is(err, catalog.ErrNoDevice):
		http.NotFound(w, r)
		return
	case err != nil:
		pageError(w, r, err)
		return
	case !d.Revoked.IsZero():
		s.renderDevice(w, r, id, false, deviceProblems["revoked"], http.StatusConflict)
		return
	}
	// The form names a row of the page; the row must still be a refused
	// project an allow rule can let through, in the newest inventory the
	// device sent. The fields alone prove nothing about its verdict.
	row, found, err := s.refusedRow(r, d.ID, r.PostForm.Get("git_remote"), r.PostForm.Get("cwd"))
	switch {
	case err != nil:
		pageError(w, r, err)
		return
	case !found:
		s.renderDevice(w, r, id, false, "That project is not refused in the newest inventory "+d.Name+" sent. Reload the page to see what it holds now.", http.StatusConflict)
		return
	case !allowable(row.Reason):
		s.renderDevice(w, r, id, false, "An allow rule cannot let that project through: "+row.Reason+".", http.StatusConflict)
		return
	}
	rule, ok := allowRule(row.GitRemote, row.CWD)
	if !ok {
		s.renderDevice(w, r, id, false, "That project has no git remote or cwd to allow.", http.StatusBadRequest)
		return
	}
	name := d.Profile
	if name == "" {
		name = config.DefaultProfile
	}
	cur, rev, stored, err := s.currentProfile(r, name)
	if err != nil {
		pageError(w, r, err)
		return
	}
	// A rule already there may cover it, as a cwd prefix above it or a
	// remote prefix does, without being the rule this would add.
	if (config.Projects{Allow: cur.Projects.Allow}).Permitted(config.ProjectID{CWD: row.CWD, CWDHash: row.CWDHash, GitRemote: row.GitRemote}) {
		s.renderDevice(w, r, id, false, "Profile "+name+" already allows that project. The device picks the rule up with its next profile fetch, unless its own config.json sets its allow rules.", http.StatusConflict)
		return
	}
	next := cur
	next.Projects.Allow = append(slices.Clone(cur.Projects.Allow), rule)
	raw, _ := json.Marshal(next)
	p, _, err := checkProfile(raw)
	if err != nil {
		s.renderDevice(w, r, id, false, "Adding the rule would make an invalid profile: "+err.Error(), http.StatusBadRequest)
		return
	}
	v := profileEditView{Name: name, Stored: stored, New: !stored && name != config.DefaultProfile, Form: formOf(p, rev)}
	v.Form.Note = "Allow " + ruleText(rule) + ", refused on " + d.Name
	v.Notice = "Adds an allow rule for " + ruleText(rule) + " to profile " + name + ", which " + d.Name + " uses. Check the devices it reaches, then save."
	if v.Preview, err = s.preview(r, name, p); err != nil {
		pageError(w, r, err)
		return
	}
	s.renderEditor(w, r, v, http.StatusOK)
}

// refusedRow is the refused project in device id's newest inventory
// with this remote and cwd; false when there is none.
func (s *Server) refusedRow(r *http.Request, id, remote, cwd string) (protocol.InventoryProject, bool, error) {
	inv, ok, err := s.catalog.DeviceInventoryOf(r.Context(), id)
	if err != nil || !ok {
		return protocol.InventoryProject{}, false, err
	}
	for _, p := range inv.Inventory.Projects {
		if !p.Allowed && p.GitRemote == remote && p.CWD == cwd {
			return p, true, nil
		}
	}
	return protocol.InventoryProject{}, false, nil
}

// ruleText names a rule the way a note or a notice says it.
func ruleText(m config.ProjectMatch) string {
	if m.GitRemote != "" {
		return "git_remote " + m.GitRemote
	}
	return "cwd_prefix " + m.CWDPrefix
}
