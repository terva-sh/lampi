package web

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

// conflictsView is the Conflicts page and a session's Conflicts tab:
// the rows, what each machine is called, and the links that switch
// between open and every conflict (TKT-01M3PTMWF6).
type conflictsView struct {
	Records catalog.Page[catalog.Record]
	// Names maps a machine id to the device bound to it.
	Names    map[string]string
	Resolved bool
	// OpenURL and AllURL list the open conflicts and every conflict.
	OpenURL, AllURL string
	// Session is set on a session's tab, where the page names no
	// session per row.
	Session string
}

// resolutionLabels are how the page names each resolution.
var resolutionLabels = map[string]string{
	catalog.ResolutionKeptHead:     "Head kept",
	catalog.ResolutionMadeHead:     "Made the head",
	catalog.ResolutionSuperseded:   "Superseded",
	catalog.ResolutionNotAConflict: "Not a conflict",
}

func resolutionLabel(r string) string {
	if l, ok := resolutionLabels[r]; ok {
		return l
	}
	return r
}

// newConflictsView builds the view for records, listed under base. A
// device list that cannot be read leaves the machines unnamed rather
// than failing the page.
func (s *Server) newConflictsView(ctx context.Context, records catalog.Page[catalog.Record], p catalog.PageRequest, base url.URL, session string) conflictsView {
	v := conflictsView{Records: records, Resolved: p.Resolved, Session: session, Names: map[string]string{}}
	// Switching lists starts again from the first page.
	q := base.Query()
	q.Del("resolved")
	q.Del("cursor")
	// Only the path and query: a link never names another origin.
	v.OpenURL = (&url.URL{Path: base.Path, RawQuery: q.Encode()}).String()
	q.Set("resolved", "true")
	v.AllURL = (&url.URL{Path: base.Path, RawQuery: q.Encode()}).String()
	devices, err := s.catalog.Devices(ctx)
	if err != nil {
		return v
	}
	// A machine a revoked device was bound to keeps that name unless an
	// active device is bound to it now.
	for _, d := range devices {
		if _, named := v.Names[d.MachineID]; d.MachineID != "" && (!named || d.Revoked.IsZero()) {
			v.Names[d.MachineID] = d.Name
		}
	}
	return v
}

const conflictsPath = "/conflicts"

func conflictURL(id string) string { return conflictsPath + "/" + url.PathEscape(id) }

// conflictRoutes mounts the operator's changes to a conflict: keep the
// head, which resolves it as kept_head, and reopen, which removes a
// resolution. Each is CSRF-checked, recorded with the signed-in operator
// as its actor, and written to the audit log before it answers, as the
// device actions are (TKT-01M3PTMWHR).
func (s *Server) conflictRoutes(m *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("POST /api/web/v1/conflicts/{id}/{action}", op(s.conflictActionAPI))
	m.Handle("POST "+conflictsPath+"/{id}/{action}", op(s.conflictActionPage))
}

// conflictDetail is one conflict as the page and the API show it.
type conflictDetail struct {
	catalog.DivergentCopy
	// Part is where the copy and the head part, when the blob store
	// could be read.
	Part *partView
	// Compared is false when this server has no blob store to compare
	// with, as opposed to a comparison that failed.
	Compared bool
}

// partView says where two files part: at byte Offset, on line Line
// (from 1). Same means they are equal, as when the copy was made the
// head. Ends names the file that ends first, every byte before equal.
// Beyond is set when no difference turned up within the bytes compared.
type partView struct {
	Offset int64  `json:"offset"`
	Line   int64  `json:"line"`
	Same   bool   `json:"same,omitempty"`
	Ends   string `json:"ends,omitempty"`
	Beyond bool   `json:"beyond,omitempty"`
}

// partCompareLimit bounds how many bytes of each side the page reads to
// find where they part. The lake compared the whole files on ingest;
// this only locates the difference for a reader.
const partCompareLimit int64 = 64 << 20

// whereTheyPart reads copy and head side by side, up to limit bytes,
// and reports the first byte that differs and the line it is on.
func whereTheyPart(ctx context.Context, blobs *cas.Store, copyDigest, headDigest string, limit int64) (partView, error) {
	if copyDigest == headDigest {
		return partView{Same: true}, nil
	}
	a, err := blobs.Open(copyDigest)
	if err != nil {
		return partView{}, err
	}
	defer a.Close()
	b, err := blobs.Open(headDigest)
	if err != nil {
		return partView{}, err
	}
	defer b.Close()
	ra, rb := bufio.NewReaderSize(io.LimitReader(a, limit), 64<<10), bufio.NewReaderSize(io.LimitReader(b, limit), 64<<10)
	var v partView
	v.Line = 1
	bufA, bufB := make([]byte, 32<<10), make([]byte, 32<<10)
	for {
		if err := ctx.Err(); err != nil {
			return partView{}, err
		}
		na, errA := io.ReadFull(ra, bufA)
		nb, errB := io.ReadFull(rb, bufB)
		if errA != nil && !endOf(errA) {
			return partView{}, errA
		}
		if errB != nil && !endOf(errB) {
			return partView{}, errB
		}
		n := min(na, nb)
		i := 0
		for i < n && bufA[i] == bufB[i] {
			i++
		}
		v.Line += int64(bytes.Count(bufA[:i], []byte("\n")))
		v.Offset += int64(i)
		switch {
		case i < n:
			return v, nil
		case na < nb:
			v.Ends = "copy"
		case nb < na:
			v.Ends = "head"
		case endOf(errA) && endOf(errB):
			if v.Offset >= limit {
				v.Beyond = true
			} else {
				v.Same = true
			}
		default:
			continue
		}
		if v.Ends != "" && v.Offset >= limit {
			v.Ends, v.Beyond = "", true
		}
		return v, nil
	}
}

func endOf(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

// loadConflict reads the conflict and, when the blob store is there,
// where its copies part. A comparison that fails leaves Part empty
// rather than failing the page.
func (s *Server) loadConflict(r *http.Request, id string) (conflictDetail, bool, error) {
	ctx, cancel := readContext(r)
	defer cancel()
	d, ok, err := s.catalog.Conflict(ctx, id)
	if err != nil || !ok {
		return conflictDetail{}, ok, err
	}
	out := conflictDetail{DivergentCopy: d}
	if s.reg != nil && s.reg.Blobs != nil {
		out.Compared = true
		if p, err := whereTheyPart(ctx, s.reg.Blobs, d.SHA256, d.HeadSHA256, partCompareLimit); err == nil {
			out.Part = &p
		} else {
			s.logError(r, "comparing a conflict's copies failed", err)
		}
	}
	return out, true, nil
}

// conflictView is the conflict page.
type conflictView struct {
	conflictDetail
	Names   map[string]string
	Problem string
	// Limit is partCompareLimit, for the page to name.
	Limit int64
}

func (s *Server) conflictPage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	s.renderConflict(w, r, r.PathValue("id"), "", http.StatusOK)
}

func (s *Server) renderConflict(w http.ResponseWriter, r *http.Request, id, problem string, status int) {
	d, ok, err := s.loadConflict(r, id)
	if err != nil {
		pageError(w, r, err)
		return
	}
	if !ok {
		pageError(w, r, sql.ErrNoRows)
		return
	}
	names := s.newConflictsView(r.Context(), catalog.Page[catalog.Record]{}, catalog.PageRequest{}, url.URL{}, "").Names
	renderStatus(w, r, pageData{Title: "Conflict", View: "conflict", AsOf: s.now().UTC().Format(time.RFC3339Nano), Conflict: conflictView{conflictDetail: d, Names: names, Problem: problem, Limit: partCompareLimit}}, status)
}

// conflictJSON is one conflict in the API.
type conflictJSON struct {
	SessionUID   string          `json:"session_uid"`
	ArtifactID   string          `json:"artifact_id"`
	Harness      string          `json:"harness"`
	Kind         string          `json:"kind"`
	RelPath      string          `json:"relpath"`
	SHA256       string          `json:"sha256"`
	Size         int64           `json:"size"`
	HeadSHA256   string          `json:"head_sha256"`
	HeadSize     int64           `json:"head_size"`
	Machines     []string        `json:"machines"`
	HeadMachines []string        `json:"head_machines"`
	Resolution   *resolutionJSON `json:"resolution,omitempty"`
	Part         *partView       `json:"part,omitempty"`
}

type resolutionJSON struct {
	Resolution string `json:"resolution"`
	ResolvedAt string `json:"resolved_at"`
	ResolvedBy string `json:"resolved_by"`
	Note       string `json:"note,omitempty"`
}

func viewConflict(d conflictDetail) conflictJSON {
	out := conflictJSON{SessionUID: d.SessionUID, ArtifactID: d.ArtifactID, Harness: d.Harness, Kind: d.Kind, RelPath: d.RelPath, SHA256: d.SHA256, Size: d.Size,
		HeadSHA256: d.HeadSHA256, HeadSize: d.HeadSize, Machines: d.Machines, HeadMachines: d.HeadMachines, Part: d.Part}
	if res := d.Resolution; res != nil {
		out.Resolution = &resolutionJSON{Resolution: res.Resolution, ResolvedAt: res.At.UTC().Format(time.RFC3339Nano), ResolvedBy: res.By, Note: res.Note}
	}
	return out
}

func (s *Server) conflictAPI(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, r, catalog.ErrPage)
		return
	}
	d, ok, err := s.loadConflict(r, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	if !ok {
		fail(w, r, sql.ErrNoRows)
		return
	}
	writeJSON(w, map[string]any{"conflict": viewConflict(d)})
}

// maxConflictNote bounds the note an operator leaves with a resolution.
const maxConflictNote = 500

// changeConflict applies action to the conflict id for the signed-in
// operator. It returns an HTTP status and an error code, which is empty
// on success. A change that stands but whose audit line did not land
// returns audit_failed.
func (s *Server) changeConflict(r *http.Request, id, action, note, head string) (int, string) {
	lake := s.reg.Lake()
	ctx := r.Context()
	// The note is checked as sent: one line of at most maxConflictNote
	// characters. Only then are surrounding spaces dropped.
	if utf8.RuneCountInString(note) > maxConflictNote || !utf8.ValidString(note) || strings.ContainsFunc(note, unicode.IsControl) {
		return http.StatusBadRequest, "invalid_note"
	}
	note = strings.TrimSpace(note)
	ident, _ := webauth.Current(r)
	who, now := actor(ident).Audit, s.now()
	var err error
	switch action {
	case "keep-head":
		err = lake.Catalog.ResolveConflict(ctx, id, catalog.ResolutionKeptHead, who, note, now)
	case "reopen":
		if note != "" {
			return http.StatusBadRequest, "invalid_note"
		}
		err = lake.Catalog.ReopenConflict(ctx, id, who, now)
	case "make-head":
		// head is the session head the operator saw, so a head that moved
		// since is never replaced unseen.
		if len(head) != 64 || s.reg.Blobs == nil {
			return http.StatusBadRequest, "invalid_request"
		}
		_, err = lake.Catalog.MakeConflictHead(ctx, s.reg.Blobs, id, head, who, note, now)
		if err == nil && s.reg.Normalize != nil {
			if _, nerr := s.reg.Normalize(ctx); nerr != nil {
				// The job row is committed; the next start or SIGHUP runs it.
				s.logError(r, "queueing normalization after a head change failed", nerr)
			}
		}
	default:
		return http.StatusNotFound, "not_found"
	}
	switch {
	case errors.Is(err, catalog.ErrNoConflict):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, catalog.ErrConflictResolved):
		return http.StatusConflict, "already_resolved"
	case errors.Is(err, catalog.ErrConflictOpen):
		return http.StatusConflict, "not_resolved"
	case errors.Is(err, catalog.ErrConflictIsHead):
		return http.StatusConflict, "is_head"
	case errors.Is(err, catalog.ErrHeadMoved):
		return http.StatusConflict, "head_moved"
	case errors.Is(err, catalog.ErrNotHeadCandidate):
		return http.StatusConflict, "not_head_candidate"
	case err != nil:
		s.logError(r, "changing a conflict failed", err)
		return http.StatusInternalServerError, "action_failed"
	}
	// The change and its event committed together. A line that cannot
	// be written now stays queued for the next flush.
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		s.logError(r, "changed a conflict but the audit line failed", err)
		return http.StatusInternalServerError, "audit_failed"
	}
	return http.StatusOK, ""
}

// conflictProblems says what to do about each refusal the page can meet.
var conflictProblems = map[string]string{
	"invalid_note":       "A note is one line of at most 500 characters.",
	"already_resolved":   "Someone resolved this conflict already. Its resolution is shown below.",
	"not_resolved":       "This conflict is open already.",
	"is_head":            "This copy is the session's head now, so it cannot be reopened as a conflict with it.",
	"head_moved":         "The session's head changed since this page was loaded, so nothing was changed. Check the copies below again.",
	"not_head_candidate": "This copy cannot be the session's head: it is a companion file of the head, such as a subagent transcript, or another kind of file.",
	"invalid_request":    "The request was not complete. Reload the page and try again.",
	"audit_failed":       "The change stands, but writing it to the audit log failed. The line stays queued. Operator logs hold the details.",
	"action_failed":      "The change failed. Operator logs hold the details.",
}

func (s *Server) conflictActionPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	id := r.PathValue("id")
	status, code := s.changeConflict(r, id, r.PathValue("action"), r.PostForm.Get("note"), r.PostForm.Get("head"))
	switch code {
	case "":
		http.Redirect(w, r, conflictURL(id), http.StatusSeeOther)
	case "not_found":
		pageError(w, r, sql.ErrNoRows)
	default:
		s.renderConflict(w, r, id, conflictProblems[code], status)
	}
}

// conflictFields are the body fields each action takes. A field named
// is refused on an action that does not take it, whatever its value,
// null included.
var conflictFields = map[string][]string{"keep-head": {"note"}, "make-head": {"note", "head"}, "reopen": nil}

func (s *Server) conflictActionAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	// keep-head takes {"note": TEXT} or no body; make-head takes
	// {"head": DIGEST, "note": TEXT}; reopen takes no body, or an empty
	// object.
	// A pointer, so a body of null is told apart from none, as the
	// device actions do.
	var req *map[string]json.RawMessage
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(&req)
	empty := errors.Is(err, io.EOF)
	if err != nil && !empty || !empty && req == nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var fields map[string]json.RawMessage
	if req != nil {
		fields = *req
	}
	id := r.PathValue("id")
	action := r.PathValue("action")
	for k := range fields {
		if !slices.Contains(conflictFields[action], k) {
			code := "invalid_request"
			if k == "note" {
				code = "invalid_note"
			}
			apiError(w, http.StatusBadRequest, code)
			return
		}
	}
	var note, head string
	if raw, ok := fields["note"]; ok && json.Unmarshal(raw, &note) != nil {
		apiError(w, http.StatusBadRequest, "invalid_note")
		return
	}
	if raw, ok := fields["head"]; ok && json.Unmarshal(raw, &head) != nil {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	status, code := s.changeConflict(r, id, action, note, head)
	if code == "not_found" || code == "invalid_note" || code == "invalid_request" {
		apiError(w, status, code)
		return
	}
	body := map[string]any{}
	if d, ok, err := s.loadConflict(r, id); err == nil && ok {
		body["conflict"] = viewConflict(d)
	}
	if code != "" {
		body["error"] = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
