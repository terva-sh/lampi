package web

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/webauth"
)

// Raw artifact reads for admins (TKT-01M3NKY2V3). The bytes are what
// an agent uploaded: ruleset v2 quarantines a hit but never rewrites
// the file, so an acknowledged secret is still in them. Only admins
// reach these routes, a read names a digest the session links to, and
// every read is queued to the audit log before a byte is sent.

// RawReadCap is the most one raw response carries. A larger artifact is
// read in several Range requests.
const RawReadCap int64 = 8 << 20

// RawTruncatedHeader is set on a response that stops at RawReadCap
// before the range the caller asked for ends. Its value is the
// artifact's full length.
const RawTruncatedHeader = "Lampi-Raw-Truncated"

func rawPath(uid string) string { return "/sessions/" + uid + "/raw" }

func (s *Server) rawRoutes(m *http.ServeMux) {
	if s.reg == nil || s.reg.Blobs == nil {
		return
	}
	admin := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.AdminOnly(h)) }
	m.Handle("GET /sessions/{uid}/raw", admin(s.rawPage))
	m.Handle("GET /sessions/{uid}/raw/{digest}", admin(s.rawArtifact))
	s.readTokenRoutes(m)
}

// rawView is the raw page: the session and its current artifacts.
type rawView struct {
	Artifacts []catalog.ArtifactRow
	CapMiB    int64
}

func (s *Server) rawEnabled() bool { return s.reg != nil && s.reg.Blobs != nil }

func (s *Server) rawPage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	uid := r.PathValue("uid")
	summary, err := s.catalog.DashboardSession(ctx, uid)
	if err != nil {
		pageError(w, r, err)
		return
	}
	all, err := s.catalog.Artifacts(ctx, uid)
	if err != nil {
		pageError(w, r, err)
		return
	}
	v := rawView{CapMiB: RawReadCap >> 20}
	for _, a := range all {
		if a.Current {
			v.Artifacts = append(v.Artifacts, a)
		}
	}
	render(w, r, pageData{Title: "Raw artifacts", View: "raw", Session: summary, Raw: v})
}

// rawArtifact sends one artifact's bytes as a download. A digest the
// session does not link to is 404, the same answer as a session that
// is not there, so the route cannot be used to probe the store.
func (s *Server) rawArtifact(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	id, _ := webauth.Current(r)
	s.serveRaw(w, r, r.PathValue("uid"), r.PathValue("digest"), actor(id).Audit)
}

// serveRaw is the read both the dashboard and raw-read tokens use.
// actor names the reader in the audit log.
func (s *Server) serveRaw(w http.ResponseWriter, r *http.Request, uid, digest, actor string) {
	ctx, cancel := readContext(r)
	defer cancel()
	a, ok, err := s.sessionArtifact(ctx, uid, digest)
	if err != nil {
		s.logError(r, "reading artifacts for a raw read failed", err)
		apiError(w, http.StatusInternalServerError, "read_failed")
		return
	}
	if !ok {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	blobs := s.reg.Blobs
	size, err := blobs.Size(digest)
	if err != nil {
		s.logError(r, "sizing a raw artifact failed", err)
		apiError(w, http.StatusInternalServerError, "read_failed")
		return
	}
	// Several Range fields are several ranges, which this route refuses
	// like a comma-separated list.
	ranges := r.Header.Values("Range")
	header := ""
	if len(ranges) == 1 {
		header = ranges[0]
	}
	start, end, ranged, ok := parseRange(header, size)
	if !ok || len(ranges) > 1 {
		w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
		apiError(w, http.StatusRequestedRangeNotSatisfiable, "invalid_range")
		return
	}
	truncated := end-start > RawReadCap
	if truncated {
		end = start + RawReadCap
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": rawFilename(a, uid)}))
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(end-start, 10))
	if truncated {
		h.Set(RawTruncatedHeader, strconv.FormatInt(size, 10))
	}
	status := http.StatusOK
	if ranged || truncated {
		status = http.StatusPartialContent
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, size))
	}
	if r.Method == http.MethodHead {
		// No byte leaves, so there is no read to record.
		w.WriteHeader(status)
		return
	}
	// Read the whole response, at most RawReadCap, before recording it,
	// so the audit line only ever names bytes that were read.
	body, err := readRange(blobs, digest, start, end)
	if err != nil {
		s.logError(r, "reading a raw artifact failed", err)
		clearDownload(h)
		apiError(w, http.StatusInternalServerError, "read_failed")
		return
	}
	lake := s.reg.Lake()
	detail := fmt.Sprintf("session=%s sha256=%s bytes=%d-%d/%d", uid, digest, start, end-1, size)
	if size == 0 {
		detail = fmt.Sprintf("session=%s sha256=%s bytes=0/0", uid, digest)
	}
	// The event is durable in the outbox before a byte leaves. A read
	// that cannot be recorded is not served.
	if err := lake.Catalog.QueueAudit(ctx, s.now(), audit.Event{Kind: audit.ArtifactRead, Actor: actor, Detail: detail}); err != nil {
		s.logError(r, "queueing a raw read's audit line failed", err)
		clearDownload(h)
		apiError(w, http.StatusInternalServerError, "audit_failed")
		return
	}
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		// Queued is recorded; the line reaches audit.jsonl on the next flush.
		s.logError(r, "a raw read's audit line stays queued", err)
	}
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		s.logError(r, "sending a raw artifact stopped", err)
	}
}

// readRange reads bytes [start, end) of digest. An object shorter than
// its recorded size is an error, not a short read.
func readRange(blobs *cas.Store, digest string, start, end int64) ([]byte, error) {
	rc, err := blobs.Open(digest)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	if _, err := io.CopyN(io.Discard, rc, start); err != nil {
		return nil, err
	}
	body := make([]byte, end-start)
	if _, err := io.ReadFull(rc, body); err != nil {
		return nil, err
	}
	return body, nil
}

// clearDownload removes the download headers set before a read that
// then failed, so the error is not offered as a file.
func clearDownload(h http.Header) {
	for _, k := range []string{"Content-Disposition", "Accept-Ranges", "Content-Length", "Content-Range", RawTruncatedHeader} {
		h.Del(k)
	}
}

// sessionArtifact finds digest among the artifacts uid links to,
// current or not. ok is false when it links to none, or when uid is no
// session.
func (s *Server) sessionArtifact(ctx context.Context, uid, digest string) (catalog.ArtifactRow, bool, error) {
	all, err := s.catalog.Artifacts(ctx, uid)
	if err != nil {
		return catalog.ArtifactRow{}, false, err
	}
	for _, a := range all {
		if a.SHA256 == digest {
			return a, true, nil
		}
	}
	return catalog.ArtifactRow{}, false, nil
}

// parseRange reads a single byte range against size. With no header
// it is the whole artifact. ranged is false then. ok is false for a
// range the artifact cannot satisfy or a header this route does not
// accept, such as several ranges.
func parseRange(header string, size int64) (start, end int64, ranged, ok bool) {
	if header == "" {
		return 0, size, false, true
	}
	spec, found := strings.CutPrefix(header, "bytes=")
	if !found || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false, false
	}
	switch {
	case first == "":
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 || size == 0 {
			return 0, 0, false, false
		}
		return max(size-n, 0), size, true, true
	default:
		a, err := strconv.ParseInt(first, 10, 64)
		if err != nil || a < 0 || a >= size {
			return 0, 0, false, false
		}
		b := size - 1
		if last != "" {
			b, err = strconv.ParseInt(last, 10, 64)
			if err != nil || b < a {
				return 0, 0, false, false
			}
			b = min(b, size-1)
		}
		return a, b + 1, true, true
	}
}

// rawFilename is the artifact's base name behind a short session id,
// reduced to characters every filesystem accepts.
func rawFilename(a catalog.ArtifactRow, uid string) string {
	base := path.Base(strings.ReplaceAll(a.RelPath, "\\", "/"))
	clean := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		}
		return '_'
	}, base)
	clean = strings.TrimLeft(clean, ".")
	if clean == "" || clean == "_" {
		clean = a.SHA256[:min(12, len(a.SHA256))]
	}
	if len(clean) > 120 {
		clean = clean[len(clean)-120:]
	}
	return strings.ToLower(uid[:min(10, len(uid))]) + "-" + clean
}
