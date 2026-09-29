package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

// Hide and unhide on the review queue: the operator's record that a
// project was reviewed and will not be imported. A hide is lake-wide,
// keyed as an allow rule would name the project, changes only what the
// dashboard shows, and sends nothing to agents: the agent goes on
// refusing the project because nothing allows it. See TKT-01M3N8FHXP.

const (
	// maxReviewBatch bounds the projects one hide, unhide or allow
	// names.
	maxReviewBatch = 500
	// maxReviewForm bounds such a form or API body.
	maxReviewForm = 1 << 20
)

func (s *Server) reviewRoutes(m *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("POST "+reviewPath+"/{action}", op(s.reviewHidePage))
	m.Handle("POST /api/web/v1/review/{action}", op(s.reviewHideAPI))
}

// parseKey reads a key as ProjectKey.String writes it: the kind, one
// space, then the key.
func parseKey(s string) (catalog.ProjectKey, bool) {
	kind, key, ok := strings.Cut(s, " ")
	k := catalog.ProjectKey{Kind: kind, Key: key}
	return k, ok && k.Valid()
}

// changeHides hides or unhides keys for the signed-in operator. It
// returns the keys it changed, an HTTP status, and an error code that is
// empty on success.
func (s *Server) changeHides(r *http.Request, action string, keys []catalog.ProjectKey, note string) ([]catalog.ProjectKey, int, string) {
	if action != "hide" && action != "unhide" {
		return nil, http.StatusNotFound, "not_found"
	}
	if len(keys) == 0 || len(keys) > maxReviewBatch {
		return nil, http.StatusBadRequest, "invalid_request"
	}
	note, ok := cleanNote(note)
	if !ok {
		return nil, http.StatusBadRequest, "invalid_note"
	}
	if action == "unhide" && note != "" {
		return nil, http.StatusBadRequest, "invalid_request"
	}
	id, _ := webauth.Current(r)
	lake := s.reg.Lake()
	var changed []catalog.ProjectKey
	var err error
	if action == "hide" {
		changed, err = lake.Catalog.HideProjects(r.Context(), keys, actor(id).Audit, note, s.now())
	} else {
		changed, err = lake.Catalog.UnhideProjects(r.Context(), keys, actor(id).Audit, s.now())
	}
	switch {
	case errors.Is(err, catalog.ErrProjectKey):
		return nil, http.StatusBadRequest, "invalid_request"
	case errors.Is(err, catalog.ErrHideNote):
		return nil, http.StatusBadRequest, "invalid_note"
	case err != nil:
		s.logError(r, "changing hidden projects failed", err)
		return nil, http.StatusInternalServerError, "action_failed"
	}
	if err := lake.Catalog.FlushAudit(r.Context(), lake.Dir); err != nil {
		s.logError(r, "changed hidden projects but the audit line failed", err)
		return changed, http.StatusInternalServerError, "audit_failed"
	}
	return changed, http.StatusOK, ""
}

// hideProblems says what to do about each refusal the forms can meet.
var hideProblems = map[string]string{
	"invalid_request": "Choose at least one project, and no more than 500 at once.",
	"invalid_note":    fmt.Sprintf("A note is at most %d characters.", maxProfileNote),
	"action_failed":   "The change failed. Operator logs hold the details.",
	"audit_failed":    "The change stands, but writing it to the audit log failed. The line stays queued. Operator logs hold the details.",
}

func (s *Server) reviewHidePage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewForm)
	if r.ParseForm() != nil || !s.auth.CheckWrite(r, r.PostForm.Get("csrf")) {
		renderStatus(w, r, pageData{Title: "Request refused", View: "refused"}, http.StatusForbidden)
		return
	}
	action := r.PathValue("action")
	if action != "hide" && action != "unhide" {
		http.NotFound(w, r)
		return
	}
	var keys []catalog.ProjectKey
	bad := false
	for _, raw := range r.PostForm["key"] {
		k, ok := parseKey(raw)
		bad = bad || !ok
		keys = append(keys, k)
	}
	back := returnPath(r.PostForm.Get("return"))
	status, code := http.StatusBadRequest, "invalid_request"
	if !bad {
		_, status, code = s.changeHides(r, action, keys, r.PostForm.Get("note"))
	}
	if code == "" {
		if back == "" {
			back = reviewPath
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	// Show the page the form was on, with what went wrong.
	if id, ok := strings.CutPrefix(back, devicesPath+"/"); ok {
		id, refused := strings.CutSuffix(id, "?show=refused")
		s.renderDevice(w, r, id, refused, hideProblems[code], status)
		return
	}
	f := reviewFilter{}
	if u, err := url.Parse(back); err == nil && back != "" {
		f, _ = parseReviewFilter(u.Query())
	}
	s.renderReview(w, r, f, hideProblems[code], "", 0, status)
}

// hideRequest is the body of POST /api/web/v1/review/{hide,unhide}.
type hideRequest struct {
	Keys []catalog.ProjectKey `json:"keys"`
	Note string               `json:"note"`
}

func (s *Server) reviewHideAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	action := r.PathValue("action")
	if action != "hide" && action != "unhide" {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	var req *hideRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxReviewForm)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req == nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	changed, status, code := s.changeHides(r, action, req.Keys, req.Note)
	body := map[string]any{"changed": changed}
	if changed == nil {
		body["changed"] = []catalog.ProjectKey{}
	}
	if code != "" {
		if status != http.StatusInternalServerError || code != "audit_failed" {
			apiError(w, status, code)
			return
		}
		body["error"] = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
