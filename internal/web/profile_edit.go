package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

// The operator's profile editor: a form of every field a profile can
// carry, a preview with the diff and the devices a save reaches, and a
// save that names the revision the operator read. The JSON API takes
// the document whole.

const (
	// maxProfileForm bounds a profile form or API body. A profile with
	// a few hundred rules fits.
	maxProfileForm = 256 << 10
	// maxRuleRows bounds the rule rows one form may post per list.
	maxRuleRows = 500
	// blankRuleRows is how many empty rows the form offers per list.
	blankRuleRows = 3
	// maxProfileNote bounds the note a save or delete records.
	maxProfileNote = 500
)

func (s *Server) profileRoutes(m *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("POST /profiles", op(s.profileCreatePage))
	m.Handle("GET /profiles/{name}/edit", op(s.profileEditPage))
	m.Handle("POST /profiles/{name}/preview", op(s.profilePreviewPage))
	m.Handle("POST /profiles/{name}/save", op(s.profileSavePage))
	m.Handle("POST /profiles/{name}/delete", op(s.profileDeletePage))
	m.Handle("PUT /api/web/v1/profiles/{name}", op(s.profilePutAPI))
	m.Handle("DELETE /api/web/v1/profiles/{name}", op(s.profileDeleteAPI))
}

// profileForm is the editor's fields as the operator left them.
type profileForm struct {
	Base        int64
	Allow       []config.ProjectMatch
	Deny        []config.ProjectMatch
	Harnesses   []harnessChoice
	Debounce    string
	DebounceMax string
	Note        string
}

// harnessChoice is one harness toggle: "" leaves the agent's setting,
// "on" or "off" sets it.
type harnessChoice struct {
	ID, Value string
}

// diffLine is one line of the indented documents' diff: Op is " ", "+"
// or "-".
type diffLine struct {
	Op, Text string
}

type profilePreview struct {
	// Document is the canonical document a save stores.
	Document string
	Diff     []diffLine
	Changed  []string
	// Unchanged is a form that changes nothing.
	Unchanged bool
	Devices   []profileDevice
	// LocalAllow counts the devices the allow rules do not reach, shown
	// when the allow rules change.
	LocalAllow int
}

type profileEditView struct {
	Name    string
	New     bool
	Stored  bool
	CSRF    string
	Problem string
	Form    profileForm
	Preview *profilePreview
}

// normalizeRules trims each rule, folds git remotes as the agent
// compares them, and drops rules with no field left.
func normalizeRules(in []config.ProjectMatch) []config.ProjectMatch {
	var out []config.ProjectMatch
	for _, r := range in {
		r = config.ProjectMatch{
			CWDPrefix:       strings.TrimSpace(r.CWDPrefix),
			GitRemote:       config.NormalizeRemote(r.GitRemote),
			GitRemotePrefix: config.NormalizeRemote(r.GitRemotePrefix),
			CWDHash:         strings.ToLower(strings.TrimSpace(r.CWDHash)),
		}
		if r != (config.ProjectMatch{}) {
			out = append(out, r)
		}
	}
	return out
}

// normalizeProfile is what every save stores: rules folded, and the
// agent fields trimmed.
func normalizeProfile(p config.Profile) config.Profile {
	p.Projects.Allow = normalizeRules(p.Projects.Allow)
	p.Projects.Deny = normalizeRules(p.Projects.Deny)
	p.Agent.Debounce = strings.TrimSpace(p.Agent.Debounce)
	p.Agent.DebounceMax = strings.TrimSpace(p.Agent.DebounceMax)
	return p
}

// checkProfile validates raw as the agent would and returns the
// document a save stores.
func checkProfile(raw []byte) (config.Profile, []byte, error) {
	p, err := config.ParseProfile(raw)
	if err != nil {
		return config.Profile{}, nil, err
	}
	p = normalizeProfile(p)
	doc, err := json.Marshal(p)
	if err != nil {
		return config.Profile{}, nil, err
	}
	// Normalizing can empty a debounce the parse accepted; parse again.
	if _, err := config.ParseProfile(doc); err != nil {
		return config.Profile{}, nil, err
	}
	return p, doc, nil
}

// formOf lays profile p out for the editor, with blank rows to add
// rules.
func formOf(p config.Profile, base int64) profileForm {
	f := profileForm{Base: base, Debounce: p.Agent.Debounce, DebounceMax: p.Agent.DebounceMax}
	f.Allow = append(append([]config.ProjectMatch{}, p.Projects.Allow...), make([]config.ProjectMatch, blankRuleRows)...)
	f.Deny = append(append([]config.ProjectMatch{}, p.Projects.Deny...), make([]config.ProjectMatch, blankRuleRows)...)
	for _, id := range profileHarnessIDs {
		c := harnessChoice{ID: id}
		if h, ok := p.Harnesses[id]; ok {
			c.Value = "off"
			if h.Enabled {
				c.Value = "on"
			}
		}
		f.Harnesses = append(f.Harnesses, c)
	}
	return f
}

// readRules reads the rows list.N.field for N below list_rows.
func readRules(v url.Values, list string) ([]config.ProjectMatch, error) {
	n, err := strconv.Atoi(v.Get(list + "_rows"))
	if err != nil || n < 0 || n > maxRuleRows {
		return nil, fmt.Errorf("%s: the form's row count is not valid", list)
	}
	rows := make([]config.ProjectMatch, n)
	for i := range rows {
		p := list + "." + strconv.Itoa(i) + "."
		rows[i] = config.ProjectMatch{CWDPrefix: v.Get(p + "cwd_prefix"), GitRemote: v.Get(p + "git_remote"),
			GitRemotePrefix: v.Get(p + "git_remote_prefix"), CWDHash: v.Get(p + "cwd_hash")}
	}
	return rows, nil
}

// readProfileForm reads the editor's fields into the profile they
// describe, and the form to show again with the rules normalized.
func readProfileForm(v url.Values) (profileForm, config.Profile, error) {
	var f profileForm
	var p config.Profile
	var err error
	if f.Base, err = strconv.ParseInt(v.Get("base"), 10, 64); err != nil || f.Base < 0 {
		return f, p, errors.New("the form's revision is not valid; reload the editor")
	}
	if f.Allow, err = readRules(v, "allow"); err != nil {
		return f, p, err
	}
	if f.Deny, err = readRules(v, "deny"); err != nil {
		return f, p, err
	}
	f.Debounce, f.DebounceMax, f.Note = v.Get("debounce"), v.Get("debounce_max"), v.Get("note")
	p.Projects = config.Projects{Allow: f.Allow, Deny: f.Deny}
	p.Agent = config.AgentConfig{Debounce: f.Debounce, DebounceMax: f.DebounceMax}
	for _, id := range profileHarnessIDs {
		c := harnessChoice{ID: id, Value: v.Get("harness." + id)}
		switch c.Value {
		case "":
		case "on", "off":
			if p.Harnesses == nil {
				p.Harnesses = config.Harnesses{}
			}
			p.Harnesses[id] = config.HarnessConfig{Enabled: c.Value == "on"}
		default:
			return f, p, fmt.Errorf("harness %s: choose not set, enabled or disabled", id)
		}
		f.Harnesses = append(f.Harnesses, c)
	}
	p = normalizeProfile(p)
	// Show the rules as they will be saved, with room to add more.
	f.Allow = append(append([]config.ProjectMatch{}, p.Projects.Allow...), make([]config.ProjectMatch, blankRuleRows)...)
	f.Deny = append(append([]config.ProjectMatch{}, p.Projects.Deny...), make([]config.ProjectMatch, blankRuleRows)...)
	return f, p, nil
}

// cleanNote keeps a note to one bounded line of printable text.
func cleanNote(s string) (string, bool) {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s))
	return s, len(s) <= maxProfileNote
}

// lineDiff is the line diff of a and b by longest common subsequence.
// Documents too large for it come out as all removed, then all added.
func lineDiff(a, b []string) []diffLine {
	if len(a)*len(b) > 1<<22 {
		var out []diffLine
		for _, l := range a {
			out = append(out, diffLine{"-", l})
		}
		for _, l := range b {
			out = append(out, diffLine{"+", l})
		}
		return out
	}
	// lcs[i][j] is the common length of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{" ", a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{"-", a[i]})
			i++
		default:
			out = append(out, diffLine{"+", b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, diffLine{"-", a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, diffLine{"+", b[j]})
	}
	return out
}

func indentLines(p config.Profile) []string {
	raw, _ := json.MarshalIndent(p, "", "  ")
	return strings.Split(string(raw), "\n")
}

// currentProfile is the stored profile called name and its revision:
// an empty profile at 0 when none is stored.
func (s *Server) currentProfile(r *http.Request, name string) (config.Profile, int64, bool, error) {
	p, err := s.catalog.ProfileByName(r.Context(), name)
	if errors.Is(err, catalog.ErrNoProfile) {
		return config.Profile{}, 0, false, nil
	}
	if err != nil {
		return config.Profile{}, 0, false, err
	}
	return p.Config, p.Revision, true, nil
}

func (s *Server) renderEditor(w http.ResponseWriter, r *http.Request, v profileEditView, status int) {
	_, v.CSRF = webauth.Current(r)
	title := "Edit profile " + v.Name
	if v.New {
		title = "New profile " + v.Name
	}
	renderStatus(w, r, pageData{Title: title, View: "profile-edit", AsOf: stampOf(s.now()), ProfileEdit: v}, status)
}

// readProfileBody parses a profile form POST, whose body can outgrow
// readForm's, and checks its CSRF token.
func (s *Server) readProfileBody(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileForm)
	if r.ParseForm() != nil || !s.auth.CheckWrite(r, r.PostForm.Get("csrf")) {
		renderStatus(w, r, pageData{Title: "Request refused", View: "refused"}, http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) profileCreatePage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !config.ValidProfileName(name) {
		s.renderProfiles(w, r, "A profile name is lowercase letters, digits, '-' and '_', at most 32 characters.", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, profileURL(name)+"/edit", http.StatusSeeOther)
}

func (s *Server) profileEditPage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !config.ValidProfileName(name) || len(r.URL.Query()) != 0 {
		http.NotFound(w, r)
		return
	}
	p, rev, stored, err := s.currentProfile(r, name)
	if err != nil {
		pageError(w, r, err)
		return
	}
	s.renderEditor(w, r, profileEditView{Name: name, New: !stored && name != config.DefaultProfile, Stored: stored, Form: formOf(p, rev)}, http.StatusOK)
}

// preview builds the preview of saving p over the stored profile.
func (s *Server) preview(r *http.Request, name string, p config.Profile) (*profilePreview, error) {
	cur, _, _, err := s.currentProfile(r, name)
	if err != nil {
		return nil, err
	}
	doc, _ := json.Marshal(p)
	pv := &profilePreview{Document: string(doc), Diff: lineDiff(indentLines(cur), indentLines(p)), Changed: catalog.ChangedProfileFields(cur, p)}
	pv.Unchanged = cur.Version() == p.Version()
	users, err := s.profileUsers(r.Context())
	if err != nil {
		return nil, err
	}
	pv.Devices = users[name]
	for _, c := range pv.Changed {
		if c != "projects.allow" {
			continue
		}
		for _, d := range pv.Devices {
			if d.AllowSource == config.OriginLocal {
				pv.LocalAllow++
			}
		}
	}
	return pv, nil
}

func (s *Server) profilePreviewPage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !config.ValidProfileName(name) {
		http.NotFound(w, r)
		return
	}
	if !s.readProfileBody(w, r) {
		return
	}
	_, _, stored, err := s.currentProfile(r, name)
	if err != nil {
		pageError(w, r, err)
		return
	}
	v := profileEditView{Name: name, Stored: stored, New: !stored && name != config.DefaultProfile}
	f, p, err := readProfileForm(r.PostForm)
	v.Form = f
	if err == nil {
		raw, _ := json.Marshal(p)
		p, _, err = checkProfile(raw)
	}
	if err != nil {
		v.Problem = err.Error()
		s.renderEditor(w, r, v, http.StatusBadRequest)
		return
	}
	if v.Preview, err = s.preview(r, name, p); err != nil {
		pageError(w, r, err)
		return
	}
	s.renderEditor(w, r, v, http.StatusOK)
}

// saveProfile stores doc as the profile called name for the signed-in
// operator, if the profile is still at revision base. It returns the
// saved profile, an HTTP status, an error code (empty on success) and
// a message for a refused document.
func (s *Server) saveProfile(r *http.Request, name string, raw []byte, base int64, note string) (catalog.Profile, int, string, string) {
	if !config.ValidProfileName(name) {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_name", ""
	}
	note, ok := cleanNote(note)
	if !ok {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_note", fmt.Sprintf("A note is at most %d characters.", maxProfileNote)
	}
	_, doc, err := checkProfile(raw)
	if err != nil {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_profile", err.Error()
	}
	id, _ := webauth.Current(r)
	lake := s.reg.Lake()
	p, _, err := lake.Catalog.PutProfileIf(r.Context(), name, doc, actor(id).Audit, note, base, s.now())
	switch {
	case errors.Is(err, catalog.ErrProfileChanged):
		return p, http.StatusConflict, "changed", ""
	case err != nil:
		s.logError(r, "saving a profile failed", err)
		return catalog.Profile{}, http.StatusInternalServerError, "save_failed", ""
	}
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.logError(r, "saved a profile but the audit line failed", err)
		return p, http.StatusInternalServerError, "audit_failed", ""
	}
	return p, http.StatusOK, "", ""
}

// profileProblems says what to do about each refusal the editor meets.
var profileProblems = map[string]string{
	"changed":       "Someone saved or deleted this profile after you opened it. Your changes are below, previewed against what is saved now; check them and save again.",
	"audit_failed":  "The profile is saved, but writing it to the audit log failed. The line stays queued. Operator logs hold the details.",
	"save_failed":   "Saving failed. Operator logs hold the details.",
	"in_use":        "Devices still use this profile. Set them to another profile on Devices first.",
	"default":       "The default profile cannot be deleted.",
	"not_found":     "There is no such profile.",
	"delete_failed": "Deleting failed. Operator logs hold the details.",
}

func (s *Server) profileSavePage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !config.ValidProfileName(name) {
		http.NotFound(w, r)
		return
	}
	if !s.readProfileBody(w, r) {
		return
	}
	base, err := strconv.ParseInt(r.PostForm.Get("base"), 10, 64)
	if err != nil {
		base = -1
	}
	raw := []byte(r.PostForm.Get("document"))
	_, status, code, msg := s.saveProfile(r, name, raw, base, r.PostForm.Get("note"))
	if code == "" {
		http.Redirect(w, r, profileURL(name), http.StatusSeeOther)
		return
	}
	if msg == "" {
		msg = profileProblems[code]
	}
	// Show the editor again with what was sent, previewed against what
	// is stored now.
	cur, rev, stored, cerr := s.currentProfile(r, name)
	if cerr != nil {
		pageError(w, r, cerr)
		return
	}
	v := profileEditView{Name: name, Stored: stored, New: !stored && name != config.DefaultProfile, Problem: msg, Form: formOf(cur, rev)}
	if p, _, perr := checkProfile(raw); perr == nil {
		v.Form = formOf(p, rev)
		v.Form.Note = r.PostForm.Get("note")
		if code == "changed" {
			if v.Preview, cerr = s.preview(r, name, p); cerr != nil {
				pageError(w, r, cerr)
				return
			}
		}
	}
	if code == "audit_failed" {
		// The save stands: show what is stored, not a form to resend.
		v.Form, v.Preview = formOf(cur, rev), nil
	}
	s.renderEditor(w, r, v, status)
}

// deleteProfile removes name for the signed-in operator, if it is
// still at revision base.
func (s *Server) deleteProfile(r *http.Request, name string, base int64, note string) (int, string) {
	note, ok := cleanNote(note)
	if !ok {
		return http.StatusBadRequest, "invalid_note"
	}
	id, _ := webauth.Current(r)
	lake := s.reg.Lake()
	_, err := lake.Catalog.DeleteProfileIf(r.Context(), name, actor(id).Audit, note, base, s.now())
	switch {
	case errors.Is(err, catalog.ErrDefaultProfile):
		return http.StatusConflict, "default"
	case errors.Is(err, catalog.ErrNoProfile):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, catalog.ErrProfileInUse):
		return http.StatusConflict, "in_use"
	case errors.Is(err, catalog.ErrProfileChanged):
		return http.StatusConflict, "changed"
	case err != nil:
		s.logError(r, "deleting a profile failed", err)
		return http.StatusInternalServerError, "delete_failed"
	}
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.logError(r, "deleted a profile but the audit line failed", err)
		return http.StatusInternalServerError, "audit_failed"
	}
	return http.StatusOK, ""
}

func (s *Server) profileDeletePage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !config.ValidProfileName(name) {
		http.NotFound(w, r)
		return
	}
	if !s.readForm(w, r) {
		return
	}
	base, err := strconv.ParseInt(r.PostForm.Get("base"), 10, 64)
	if err != nil {
		base = -1
	}
	status, code := s.deleteProfile(r, name, base, r.PostForm.Get("note"))
	if code == "" || code == "audit_failed" {
		http.Redirect(w, r, "/profiles", http.StatusSeeOther)
		return
	}
	cur, rev, stored, cerr := s.currentProfile(r, name)
	if cerr != nil {
		pageError(w, r, cerr)
		return
	}
	msg := profileProblems[code]
	if code == "changed" {
		msg = "Someone saved this profile after you opened it. Review it below before deleting."
	}
	if code == "invalid_note" {
		msg = fmt.Sprintf("A note is at most %d characters.", maxProfileNote)
	}
	s.renderEditor(w, r, profileEditView{Name: name, Stored: stored, Problem: msg, Form: formOf(cur, rev)}, status)
}

// profileWrite is the body of PUT and DELETE on a profile.
type profileWrite struct {
	Document     json.RawMessage `json:"document"`
	BaseRevision *int64          `json:"base_revision"`
	Note         string          `json:"note"`
}

func (s *Server) readProfileWrite(w http.ResponseWriter, r *http.Request) (profileWrite, bool) {
	var req *profileWrite
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return profileWrite{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileForm)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req == nil || req.BaseRevision == nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return profileWrite{}, false
	}
	return *req, true
}

func (s *Server) profilePutAPI(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readProfileWrite(w, r)
	if !ok {
		return
	}
	if len(req.Document) == 0 || req.Document[0] != '{' {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	p, status, code, msg := s.saveProfile(r, r.PathValue("name"), req.Document, *req.BaseRevision, req.Note)
	body := map[string]any{}
	if p.Name != "" {
		body["profile"] = map[string]any{"name": p.Name, "version": p.Version, "revision": p.Revision}
	}
	if code != "" {
		body["error"] = code
	}
	if msg != "" && code == "invalid_profile" {
		body["message"] = msg
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) profileDeleteAPI(w http.ResponseWriter, r *http.Request) {
	req, ok := s.readProfileWrite(w, r)
	if !ok {
		return
	}
	if len(req.Document) != 0 {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	status, code := s.deleteProfile(r, r.PathValue("name"), *req.BaseRevision, req.Note)
	if code != "" {
		apiError(w, status, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
