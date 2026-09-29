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

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

// Allow selected: several refused projects allowed in one step. The
// lake re-reads the queue, builds the rule Allow would add for each
// device copy that still needs review, and groups the rules by the
// profile of the device holding the copy. A short page confirms what
// each profile gains and which devices it reaches. Save writes every
// profile against the revision the page read, all or none. See
// TKT-01M3N8FHZR.

// allowProfile is one profile a batch Allow changes.
type allowProfile struct {
	Name string `json:"name"`
	// Base is the revision the plan read; Stored is false for a
	// default no one has saved.
	Base   int64 `json:"base_revision"`
	Stored bool  `json:"stored"`
	// Rules are the allow rules added, each with the projects and
	// devices it is for.
	Rules []allowRuleView `json:"rules"`
	// Removed are the allow rules the profile had that the new rules
	// cover, which the plan drops.
	Removed    []config.ProjectMatch `json:"removed"`
	Reach      *profileReach         `json:"reach"`
	Document   string                `json:"-"`
	Doc        json.RawMessage       `json:"document"`
	Devices    []profileDevice       `json:"devices"`
	LocalAllow int                   `json:"local_allow"`
	Diff       []diffLine            `json:"-"`
	// Form is the profile with the rules added, laid out for the full
	// editor.
	Form profileForm `json:"-"`
}

// allowRuleView is one rule a batch adds and what it is for.
type allowRuleView struct {
	Rule    config.ProjectMatch `json:"rule"`
	Text    string              `json:"-"`
	Key     catalog.ProjectKey  `json:"key"`
	Devices []string            `json:"for_devices"`
}

// allowView is the confirm page, and the API's plan.
type allowView struct {
	Profiles []allowProfile `json:"profiles"`
	// Skipped are selected projects no rule is added for: they no
	// longer need review in the devices the page showed, or are only on
	// devices whose config.json sets their allow rules.
	Skipped     []catalog.ProjectKey `json:"skipped"`
	Keys        []string             `json:"-"`
	Width       string               `json:"-"`
	Note        string               `json:"-"`
	Return      string               `json:"-"`
	ReturnLabel string               `json:"-"`
	CSRF        string               `json:"-"`
	Problem     string               `json:"-"`
}

// filterOf is the review filter a return names: the device of a device
// page, or the review page's own filter.
func filterOf(back string) reviewFilter {
	u, err := url.Parse(back)
	if err != nil {
		return reviewFilter{}
	}
	if id, ok := strings.CutPrefix(u.Path, devicesPath+"/"); ok {
		return reviewFilter{Device: id}
	}
	// The tab stays, so a refused action shows the page it came from;
	// the queue reads the same either way.
	f, _ := parseReviewFilter(u.Query())
	return f
}

// planAllow builds what allowing keys adds, for the device copies that
// still need review under filter f.
// width is widthRepository or widthOwner.
func (s *Server) planAllow(r *http.Request, keys []catalog.ProjectKey, f reviewFilter, width string) (allowView, error) {
	v := allowView{Width: width}
	ctx, cancel := readContext(r)
	defer cancel()
	q, err := s.readReview(ctx, f, s.now())
	if err != nil {
		return v, err
	}
	needs := map[catalog.ProjectKey]reviewRow{}
	for _, row := range q.Needs {
		needs[row.Key] = row
	}
	byProfile := map[string]*allowProfile{}
	for _, k := range keys {
		v.Keys = append(v.Keys, k.String())
		row, ok := needs[k]
		if !ok {
			v.Skipped = append(v.Skipped, k)
			continue
		}
		added := false
		for _, sg := range row.Sightings {
			rule, ok := allowRuleAt(sg.Project.GitRemote, sg.Project.CWD, width)
			// A device with its own allow rules takes none from its
			// profile; a rule there would change nothing for it.
			// A still-refused copy's profile allows it already.
			// readReview files each copy under its own state, so a Needs
			// row holds only copies that need review; check anyway.
			if !ok || sg.State != stateNeeds || sg.LocalAllow || sg.StillRefused {
				continue
			}
			added = true
			ap := byProfile[sg.ProfileName]
			if ap == nil {
				ap = &allowProfile{Name: sg.ProfileName}
				byProfile[sg.ProfileName] = ap
			}
			i := slices.IndexFunc(ap.Rules, func(rv allowRuleView) bool { return rv.Rule == rule })
			if i < 0 {
				ap.Rules = append(ap.Rules, allowRuleView{Rule: rule, Text: ruleText(rule), Key: k})
				i = len(ap.Rules) - 1
			}
			// At owner width several projects share a rule, and a device
			// can hold more than one of them.
			if !slices.Contains(ap.Rules[i].Devices, sg.DeviceName) {
				ap.Rules[i].Devices = append(ap.Rules[i].Devices, sg.DeviceName)
			}
		}
		if !added {
			v.Skipped = append(v.Skipped, k)
		}
	}
	for _, name := range sortedKeys(byProfile) {
		ap := byProfile[name]
		cur, rev, stored, err := s.currentProfile(r, name)
		if err != nil {
			return v, err
		}
		next := cur
		var add []config.ProjectMatch
		for _, rv := range ap.Rules {
			add = append(add, rv.Rule)
		}
		next.Projects.Allow, ap.Removed = withRules(cur.Projects.Allow, add)
		if ap.Removed == nil {
			ap.Removed = []config.ProjectMatch{}
		}
		ap.Rules = survivingRules(ap.Rules, next.Projects.Allow)
		raw, _ := json.Marshal(next)
		p, doc, err := checkProfile(raw)
		if err != nil {
			// No partial plan: one profile left out would save the rest.
			return allowView{}, fmt.Errorf("%w: profile %s: %v", errPlan, name, err)
		}
		pv, err := s.preview(r, name, p)
		if err != nil {
			return v, err
		}
		ap.Base, ap.Stored, ap.Document, ap.Doc = rev, stored, string(doc), doc
		ap.Devices, ap.LocalAllow, ap.Diff, ap.Reach = pv.Devices, 0, pv.Diff, pv.Reach
		for _, d := range pv.Devices {
			if d.AllowSource == config.OriginLocal {
				ap.LocalAllow++
			}
		}
		if ap.Devices == nil {
			ap.Devices = []profileDevice{}
		}
		ap.Form = formOf(p, rev)
		v.Profiles = append(v.Profiles, *ap)
	}
	if v.Skipped == nil {
		v.Skipped = []catalog.ProjectKey{}
	}
	if v.Profiles == nil {
		v.Profiles = []allowProfile{}
	}
	return v, nil
}

// survivingRules is rules less those withRules left out of allow, as a
// nested owner's prefix under a wider one is. A rule left out hands its
// devices to a surviving rule that covers it, so the page still says
// which devices the selection was for.
func survivingRules(rules []allowRuleView, allow []config.ProjectMatch) []allowRuleView {
	var out []allowRuleView
	var gone []allowRuleView
	for _, rv := range rules {
		if slices.Contains(allow, rv.Rule) {
			out = append(out, rv)
		} else {
			gone = append(gone, rv)
		}
	}
	for _, g := range gone {
		for i := range out {
			if config.Covers(out[i].Rule, g.Rule) {
				for _, d := range g.Devices {
					if !slices.Contains(out[i].Devices, d) {
						out[i].Devices = append(out[i].Devices, d)
					}
				}
				break
			}
		}
	}
	return out
}

// errPlan is a plan that would make an invalid profile.
var errPlan = errors.New("the allow rules would make an invalid profile")

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// readKeys reads the form's key fields; false when one is not a key or
// there are none or too many.
func readKeys(raw []string) ([]catalog.ProjectKey, bool) {
	if len(raw) == 0 || len(raw) > maxReviewBatch {
		return nil, false
	}
	var keys []catalog.ProjectKey
	for _, s := range raw {
		k, ok := parseKey(s)
		if !ok {
			return nil, false
		}
		if !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	return keys, true
}

// readReviewForm parses a review form's body and checks its CSRF token.
func (s *Server) readReviewForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewForm)
	if r.ParseForm() != nil || !s.auth.CheckWrite(r, r.PostForm.Get("csrf")) {
		renderStatus(w, r, pageData{Title: "Request refused", View: "refused"}, http.StatusForbidden)
		return false
	}
	return true
}

// renderBack shows the page a review form came from with problem.
func (s *Server) renderBack(w http.ResponseWriter, r *http.Request, back, problem string, status int) {
	if id, ok := strings.CutPrefix(back, devicesPath+"/"); ok {
		id, refused := strings.CutSuffix(id, "?show=refused")
		s.renderDevice(w, r, id, refused, problem, status)
		return
	}
	s.renderReview(w, r, filterOf(back), problem, nil, status)
}

func (s *Server) renderAllow(w http.ResponseWriter, r *http.Request, v allowView, status int) {
	_, v.CSRF = webauth.Current(r)
	v.ReturnLabel = s.returnLabel(r, v.Return)
	projects := map[catalog.ProjectKey]bool{}
	for _, p := range v.Profiles {
		for _, rv := range p.Rules {
			projects[rv.Key] = true
		}
	}
	n := len(projects)
	if v.Note == "" {
		v.Note = fmt.Sprintf("Allow %d %s from review", n, plural(n, "project", "projects"))
	}
	renderStatus(w, r, pageData{Title: "Allow selected projects", View: "review-allow", AsOf: stampOf(s.now()), Allow: v}, status)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// reviewAllowPage answers Allow selected with the confirm page.
func (s *Server) reviewAllowPage(w http.ResponseWriter, r *http.Request) {
	if !s.readReviewForm(w, r) {
		return
	}
	back := returnPath(r.PostForm.Get("return"))
	if back == "" {
		back = reviewPath
	}
	keys, ok := readKeys(r.PostForm["key"])
	if !ok {
		s.renderBack(w, r, back, "Choose at least one project to allow, and no more than 500 at once.", http.StatusBadRequest)
		return
	}
	width, ok := readWidth(r.PostForm.Get("width"))
	if !ok {
		s.renderBack(w, r, back, "Allow each repository, or each repository's owner.", http.StatusBadRequest)
		return
	}
	v, err := s.planAllow(r, keys, filterOf(back), width)
	if errors.Is(err, errPlan) {
		s.renderBack(w, r, back, err.Error()+".", http.StatusBadRequest)
		return
	}
	if err != nil {
		pageError(w, r, err)
		return
	}
	if len(v.Profiles) == 0 {
		s.renderBack(w, r, back, "No rule can be added for the selected projects: each is allowed, hidden, denied, no longer on a device, or only on devices whose config.json sets their own allow rules or that fetch no profile (run terva-lampi lakes adopt on those). Reload to see the queue as it is now.", http.StatusConflict)
		return
	}
	v.Return = back
	s.renderAllow(w, r, v, http.StatusOK)
}

// reviewAllowSavePage saves what the confirm page showed.
func (s *Server) reviewAllowSavePage(w http.ResponseWriter, r *http.Request) {
	if !s.readReviewForm(w, r) {
		return
	}
	back := returnPath(r.PostForm.Get("return"))
	if back == "" {
		back = reviewPath
	}
	var writes []catalog.ProfileWrite
	names := r.PostForm["profile"]
	for i, name := range names {
		base, err := strconv.ParseInt(r.PostForm.Get("base."+strconv.Itoa(i)), 10, 64)
		if err != nil || !config.ValidProfileName(name) || len(names) > maxReviewBatch {
			s.renderBack(w, r, back, "The confirmation form is not valid. Select the projects again.", http.StatusBadRequest)
			return
		}
		writes = append(writes, catalog.ProfileWrite{Name: name, Document: []byte(r.PostForm.Get("document." + strconv.Itoa(i))), Base: base})
	}
	note := r.PostForm.Get("note")
	saved, status, code, msg := s.saveProfiles(r, writes, note)
	if code == "" {
		http.Redirect(w, r, savedURL(back, saved...), http.StatusSeeOther)
		return
	}
	if code == "audit_failed" {
		s.renderBack(w, r, back, profileProblems[code], status)
		return
	}
	// Show the confirm page again, planned against what is stored now.
	keys, ok := readKeys(r.PostForm["key"])
	if !ok {
		s.renderBack(w, r, back, "The confirmation form is not valid. Select the projects again.", http.StatusBadRequest)
		return
	}
	width, ok := readWidth(r.PostForm.Get("width"))
	if !ok {
		s.renderBack(w, r, back, "The confirmation form is not valid. Select the projects again.", http.StatusBadRequest)
		return
	}
	v, err := s.planAllow(r, keys, filterOf(back), width)
	if errors.Is(err, errPlan) {
		s.renderBack(w, r, back, "Nothing was saved, and "+err.Error()+".", http.StatusBadRequest)
		return
	}
	if err != nil {
		pageError(w, r, err)
		return
	}
	if len(v.Profiles) == 0 {
		s.renderBack(w, r, back, "Nothing was saved, and no rule can be added for the selected projects any more.", status)
		return
	}
	v.Return, v.Note = back, note
	v.Problem = msg
	if msg == "" {
		v.Problem = allowProblems[code]
	}
	s.renderAllow(w, r, v, status)
}

var allowProblems = map[string]string{
	"changed":      "Someone changed a profile after this page read it, so nothing was saved. Below is the plan against what is stored now; check it and save again.",
	"invalid_note": fmt.Sprintf("A note is at most %d characters. Nothing was saved.", maxProfileNote),
	"save_failed":  "Saving failed, and nothing was saved. Operator logs hold the details.",
}

// saveProfiles stores writes for the signed-in operator, all or none.
// It returns the saved profiles, an HTTP status, an error code (empty
// on success) and a message for a refused document.
func (s *Server) saveProfiles(r *http.Request, writes []catalog.ProfileWrite, note string) ([]catalog.Profile, int, string, string) {
	note, ok := cleanNote(note)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_note", ""
	}
	if len(writes) == 0 {
		return nil, http.StatusBadRequest, "invalid_request", ""
	}
	for i, wr := range writes {
		_, doc, err := checkProfile(wr.Document)
		if err != nil {
			return nil, http.StatusBadRequest, "invalid_profile", "Profile " + wr.Name + ": " + err.Error()
		}
		writes[i].Document = doc
	}
	id, _ := webauth.Current(r)
	lake := s.reg.Lake()
	ps, err := lake.Catalog.PutProfilesIf(r.Context(), writes, actor(id).Audit, note, s.now())
	switch {
	case errors.Is(err, catalog.ErrProfileChanged):
		return nil, http.StatusConflict, "changed", ""
	case errors.Is(err, catalog.ErrProfileName):
		return nil, http.StatusBadRequest, "invalid_request", ""
	case err != nil:
		s.logError(r, "saving profiles failed", err)
		return nil, http.StatusInternalServerError, "save_failed", ""
	}
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.logError(r, "saved profiles but the audit line failed", err)
		return ps, http.StatusInternalServerError, "audit_failed", ""
	}
	return ps, http.StatusOK, "", ""
}

// allowPlanRequest is the body of POST /api/web/v1/review/allow.
type allowPlanRequest struct {
	Keys   []catalog.ProjectKey `json:"keys"`
	Device string               `json:"device"`
	// Width is "repository", the default, or "owner".
	Width string `json:"width"`
}

// allowSaveRequest is the body of POST /api/web/v1/review/allow/save.
type allowSaveRequest struct {
	Profiles []struct {
		Name         string          `json:"name"`
		BaseRevision *int64          `json:"base_revision"`
		Document     json.RawMessage `json:"document"`
	} `json:"profiles"`
	Note string `json:"note"`
}

// decodeStrict reads one JSON object of known fields into v.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewForm)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func (s *Server) reviewAllowAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	var req allowPlanRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	width, ok := readWidth(req.Width)
	if !ok || len(req.Keys) == 0 || len(req.Keys) > maxReviewBatch || req.Device != "" && !validDeviceID(req.Device) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	for _, k := range req.Keys {
		if !k.Valid() {
			apiError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	v, err := s.planAllow(r, req.Keys, reviewFilter{Device: req.Device}, width)
	if errors.Is(err, errPlan) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_profile", "message": err.Error()})
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) reviewAllowSaveAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	var req allowSaveRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	if len(req.Profiles) > maxReviewBatch {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var writes []catalog.ProfileWrite
	for _, p := range req.Profiles {
		if p.BaseRevision == nil || len(p.Document) == 0 || p.Document[0] != '{' {
			apiError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		writes = append(writes, catalog.ProfileWrite{Name: p.Name, Document: p.Document, Base: *p.BaseRevision})
	}
	ps, status, code, msg := s.saveProfiles(r, writes, req.Note)
	body := map[string]any{}
	var out []map[string]any
	for _, p := range ps {
		out = append(out, map[string]any{"name": p.Name, "version": p.Version, "revision": p.Revision})
	}
	if out != nil {
		body["profiles"] = out
	}
	if code != "" {
		body["error"] = code
	}
	if msg != "" {
		body["message"] = msg
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
