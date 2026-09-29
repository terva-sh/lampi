package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	// maxRuleRows bounds the rules a dashboard save stores per list.
	maxRuleRows = 500
	// maxFormRows bounds the rows one form may post per list: more than
	// a save stores, so a profile stored some other way still opens and
	// is refused with the limit named.
	maxFormRows = 5000
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
	m.Handle("POST /profiles/{name}/rollback/{revision}", op(s.profileRollbackPage))
	m.Handle("POST /api/web/v1/profiles/{name}/rollback", op(s.profileRollbackAPI))
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
	// Unchanged is a form that changes nothing; Creates is a save that
	// makes the profile.
	Unchanged bool
	Creates   bool
	Devices   []profileDevice
	// LocalAllow counts the devices the allow rules do not reach, shown
	// when the allow rules change.
	LocalAllow int
	// Reach is what the change does to the devices' projects, set when
	// the project rules change.
	Reach *profileReach
}

type profileEditView struct {
	Name    string
	New     bool
	Stored  bool
	CSRF    string
	Problem string
	// Notice says why the editor opened with a change already made, as
	// the Allow action on a device's page opens it.
	Notice string
	// Return is the page that sent the operator here, a returnPath, and
	// ReturnLabel names it for the Back link. A save goes back to it.
	Return      string
	ReturnLabel string
	Form        profileForm
	Preview     *profilePreview
	// Covered and Folds are the editor's offers to shorten the allow
	// list, from the rules the form holds.
	Covered []coveredRule
	Folds   []ownerFold
}

// setReturn takes the form's return field, when it names a page the
// editor may go back to.
func (s *Server) setReturn(r *http.Request, v *profileEditView, raw string) {
	if v.Return = returnPath(raw); v.Return != "" {
		v.ReturnLabel = s.returnLabel(r, v.Return)
	}
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
			CWDGlob:         strings.TrimSpace(r.CWDGlob),
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
	if len(p.Projects.Allow) > maxRuleRows || len(p.Projects.Deny) > maxRuleRows {
		return config.Profile{}, nil, fmt.Errorf("projects: at most %d allow and %d deny rules", maxRuleRows, maxRuleRows)
	}
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

// withBlanks is rules and the empty rows the form offers to add more,
// as many as the row limit leaves room for.
func withBlanks(rules []config.ProjectMatch) []config.ProjectMatch {
	n := min(blankRuleRows, max(0, maxRuleRows-len(rules)))
	return append(append([]config.ProjectMatch{}, rules...), make([]config.ProjectMatch, n)...)
}

// formOf lays profile p out for the editor, with blank rows to add
// rules.
func formOf(p config.Profile, base int64) profileForm {
	f := profileForm{Base: base, Debounce: p.Agent.Debounce, DebounceMax: p.Agent.DebounceMax}
	f.Allow, f.Deny = withBlanks(p.Projects.Allow), withBlanks(p.Projects.Deny)
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
	if err != nil || n < 0 || n > maxFormRows {
		return nil, fmt.Errorf("%s: the form's row count is not valid", list)
	}
	rows := make([]config.ProjectMatch, n)
	for i := range rows {
		p := list + "." + strconv.Itoa(i) + "."
		rows[i] = config.ProjectMatch{CWDPrefix: v.Get(p + "cwd_prefix"), GitRemote: v.Get(p + "git_remote"),
			GitRemotePrefix: v.Get(p + "git_remote_prefix"), CWDHash: v.Get(p + "cwd_hash"), CWDGlob: v.Get(p + "cwd_glob")}
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
	f.Allow, f.Deny = withBlanks(p.Projects.Allow), withBlanks(p.Projects.Deny)
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
	return s, utf8.RuneCountInString(s) <= maxProfileNote
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
// an empty profile at its newest revision when none is stored.
func (s *Server) currentProfile(r *http.Request, name string) (config.Profile, int64, bool, error) {
	p, err := s.catalog.ProfileByName(r.Context(), name)
	if errors.Is(err, catalog.ErrNoProfile) {
		rev, err := s.catalog.LatestProfileRevision(r.Context(), name)
		return config.Profile{}, rev, false, err
	}
	if err != nil {
		return config.Profile{}, 0, false, err
	}
	return p.Config, p.Revision, true, nil
}

func (s *Server) renderEditor(w http.ResponseWriter, r *http.Request, v profileEditView, status int) {
	_, v.CSRF = webauth.Current(r)
	allow := normalizeRules(v.Form.Allow)
	v.Covered, v.Folds = coveredRules(allow), ownerFolds(allow)
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
	cur, _, stored, err := s.currentProfile(r, name)
	if err != nil {
		return nil, err
	}
	doc, _ := json.Marshal(p)
	pv := &profilePreview{Document: string(doc), Diff: lineDiff(indentLines(cur), indentLines(p)), Changed: catalog.ChangedProfileFields(cur, p)}
	// Saving a profile that is not stored creates it, even empty.
	pv.Unchanged = stored && cur.Version() == p.Version()
	pv.Creates = !stored
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
	if slices.ContainsFunc(pv.Changed, func(c string) bool { return c == "projects.allow" || c == "projects.deny" }) {
		st := storedProfile{Version: cur.Version()}
		if stored {
			sp, err := s.catalog.ProfileByName(r.Context(), name)
			if err != nil {
				return nil, err
			}
			st.Saved = sp.Updated
		}
		reach, err := s.reach(r.Context(), cur.Projects, p.Projects, st, pv.Devices)
		if err != nil {
			return nil, err
		}
		pv.Reach = &reach
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
	s.setReturn(r, &v, r.PostForm.Get("return"))
	f, p, err := readProfileForm(r.PostForm)
	if err == nil {
		tidyForm(r.PostForm, &f, &p)
	}
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
	note, ok := cleanNote(note)
	if !ok {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_note", fmt.Sprintf("A note is at most %d characters.", maxProfileNote)
	}
	return s.saveCleanProfile(r, name, raw, base, note)
}

// saveCleanProfile is saveProfile for a note already checked.
func (s *Server) saveCleanProfile(r *http.Request, name string, raw []byte, base int64, note string) (catalog.Profile, int, string, string) {
	if !config.ValidProfileName(name) {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_name", ""
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
	saved, status, code, msg := s.saveProfile(r, name, raw, base, r.PostForm.Get("note"))
	back := returnPath(r.PostForm.Get("return"))
	if code == "" {
		to := profileURL(name)
		if back != "" {
			to = savedURL(back, saved)
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
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
	s.setReturn(r, &v, back)
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
	if !config.ValidProfileName(name) {
		return http.StatusBadRequest, "invalid_name"
	}
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
	if code == "" {
		http.Redirect(w, r, "/profiles", http.StatusSeeOther)
		return
	}
	if code == "audit_failed" {
		s.renderProfiles(w, r, "The profile is deleted, but writing it to the audit log failed. The line stays queued. Operator logs hold the details.", status)
		return
	}
	msg := profileProblems[code]
	switch code {
	case "changed":
		msg = "Someone saved or deleted this profile after you opened it. Nothing was deleted; review it below and delete again."
	case "invalid_note":
		msg = fmt.Sprintf("A note is at most %d characters.", maxProfileNote)
	}
	if code == "changed" || code == "not_found" {
		if _, _, stored, err := s.currentProfile(r, name); err == nil && !stored {
			// Someone else deleted it: there is no profile page to show.
			s.renderProfiles(w, r, "That profile was already deleted.", status)
			return
		}
	}
	// The profile's page, as it is now, holds the delete form to retry.
	s.renderProfile(w, r, name, msg, status)
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

// rollback saves revision rev's document as the profile called name, if
// the profile is still at revision base. The note names the revision.
func (s *Server) rollback(r *http.Request, name string, rev, base int64, note string) (catalog.Profile, int, string) {
	if !config.ValidProfileName(name) {
		return catalog.Profile{}, http.StatusNotFound, "not_found"
	}
	old, err := s.catalog.ProfileRevisionByID(r.Context(), name, rev)
	switch {
	case errors.Is(err, catalog.ErrNoProfile):
		return catalog.Profile{}, http.StatusNotFound, "not_found"
	case err != nil:
		s.logError(r, "reading a profile revision failed", err)
		return catalog.Profile{}, http.StatusInternalServerError, "save_failed"
	case old.Deleted:
		return catalog.Profile{}, http.StatusBadRequest, "deleted_revision"
	}
	// The caller's note has the editor's limit; the label naming the
	// revision comes on top of it.
	note, ok := cleanNote(note)
	if !ok {
		return catalog.Profile{}, http.StatusBadRequest, "invalid_note"
	}
	label := "rollback to revision " + strconv.FormatInt(rev, 10)
	if note != "" {
		label += ": " + note
	}
	p, status, code, _ := s.saveCleanProfile(r, name, []byte(old.Document), base, label)
	return p, status, code
}

func (s *Server) profileRollbackPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	name := r.PathValue("name")
	rev, err1 := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	base, err2 := strconv.ParseInt(r.PostForm.Get("base"), 10, 64)
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	_, status, code := s.rollback(r, name, rev, base, r.PostForm.Get("note"))
	if code == "" {
		http.Redirect(w, r, profileURL(name), http.StatusSeeOther)
		return
	}
	if code == "not_found" {
		http.NotFound(w, r)
		return
	}
	msg := map[string]string{
		"changed":          "Someone saved this profile after you opened it. Nothing was rolled back; check the revisions below and try again.",
		"deleted_revision": "That revision records a deletion; choose a saved one.",
		"invalid_note":     fmt.Sprintf("A note is at most %d characters.", maxProfileNote),
	}[code]
	if msg == "" {
		msg = profileProblems[code]
	}
	s.renderProfile(w, r, name, msg, status)
}

func (s *Server) profileRollbackAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	var req *struct {
		Revision     *int64 `json:"revision"`
		BaseRevision *int64 `json:"base_revision"`
		Note         string `json:"note"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req == nil || req.Revision == nil || req.BaseRevision == nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	p, status, code := s.rollback(r, r.PathValue("name"), *req.Revision, *req.BaseRevision, req.Note)
	body := map[string]any{}
	if p.Name != "" {
		body["profile"] = map[string]any{"name": p.Name, "version": p.Version, "revision": p.Revision}
	}
	if code != "" {
		body["error"] = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
