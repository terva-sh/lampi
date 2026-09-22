// Package api is the ingest HTTP surface: health, catalog stats, hello,
// blob check, blob put, and manifests.
//
// healthz is unauthenticated and returns no catalog data, so a process
// probe does not need a device token. Every /v1 route checks the bearer
// token when any device hash is configured. An empty Devices set
// disables that check. The serve command refuses to bind a non-loopback
// address in that state. Tokens are stored as hashes, not the bearer.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// Server is one lake process's HTTP API.
type Server struct {
	CAS     *cas.Store
	Catalog *catalog.Catalog
	// Devices are SHA-256 hashes of bearer tokens. Nil or empty disables
	// the check. The plaintext is not kept on the server.
	Devices *auth.Devices
	Now     func() time.Time
}

// Allow enrolls a device token by its hash. The token string is not stored.
func (s *Server) Allow(token string) {
	if s.Devices == nil {
		s.Devices = &auth.Devices{}
	}
	s.Devices.Allow(token)
}

// Open loads a filesystem CAS and a SQLite catalog under dataDir.
func Open(dataDir string) (*Server, error) {
	store, err := cas.Open(filepath.Join(dataDir, "cas"))
	if err != nil {
		return nil, err
	}
	cat, err := catalog.Open(filepath.Join(dataDir, "catalog.db"))
	if err != nil {
		return nil, err
	}
	return &Server{CAS: store, Catalog: cat, Now: time.Now}, nil
}

// Close releases the catalog. The CAS is just a directory.
func (s *Server) Close() error {
	if s.Catalog == nil {
		return nil
	}
	return s.Catalog.Close()
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// Handler is the ingest mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /v1/stats", s.authed(s.stats))
	mux.HandleFunc("POST /v1/hello", s.authed(s.hello))
	mux.HandleFunc("POST /v1/blobs/check", s.authed(s.check))
	mux.HandleFunc("PUT /v1/blobs/{digest}", s.authed(s.put))
	mux.HandleFunc("POST /v1/manifests", s.authed(s.manifest))
	return mux
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Devices == nil || s.Devices.Empty() {
			next(w, r)
			return
		}
		if !s.Devices.Match(r.Header.Get("Authorization")) {
			writeJSON(w, http.StatusUnauthorized, protocol.ErrorBody{Error: "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	n, err := s.Catalog.Counts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, protocol.StatsResponse{
		Sessions:  n.Sessions,
		Artifacts: n.Artifacts,
		Machines:  n.Machines,
	})
}

func (s *Server) hello(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, protocol.HelloResponse{
		ServerTime:       s.now().UTC(),
		ProtocolVersions: []int{protocol.Version},
		MaxBlobBytes:     protocol.MaxBlobBytes,
	})
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	var digests []string
	if err := decodeJSON(w, r, &digests); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, d := range digests {
		d = strings.ToLower(d)
		if !protocol.ValidDigest(d) {
			writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: "invalid digest " + d})
			return
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		ok, err := s.CAS.Has(d)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, protocol.ErrorBody{Error: err.Error()})
			return
		}
		if !ok {
			missing = append(missing, d)
		}
	}
	writeJSON(w, http.StatusOK, protocol.BlobCheckResponse{Missing: missing})
}

func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	digest := strings.ToLower(r.PathValue("digest"))
	if !protocol.ValidDigest(digest) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: "invalid digest"})
		return
	}
	cr := r.Header.Get("Content-Range")
	if cr != "" && isJSON(r.Header.Get("Content-Type")) {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: "content-range and chunk digests are different puts"})
		return
	}
	if cr != "" {
		s.putRange(w, r, digest, cr)
		return
	}
	if isJSON(r.Header.Get("Content-Type")) {
		s.putChunks(w, r, digest)
		return
	}
	exists, err := s.CAS.Put(digest, r.Body, protocol.MaxBlobBytes)
	s.finishPut(w, digest, exists, true, err)
}

func (s *Server) putRange(w http.ResponseWriter, r *http.Request, digest, header string) {
	start, end, total, err := parseContentRange(header)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	exists, complete, err := s.CAS.PutRange(digest, start, end, total, protocol.MaxBlobBytes, r.Body)
	s.finishPut(w, digest, exists, complete, err)
}

func (s *Server) putChunks(w http.ResponseWriter, r *http.Request, digest string) {
	var body struct {
		ChunkSHA256s []string `json:"chunk_sha256s"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	parts, err := normalizeDigests(body.ChunkSHA256s)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	ok, err := s.CAS.Has(digest)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.ErrorBody{Error: err.Error()})
		return
	}
	if ok {
		// The digest is already stored. Do not read or rewrite the chunks.
		s.finishPut(w, digest, true, true, nil)
		return
	}
	var missing []string
	seen := map[string]bool{}
	for _, p := range parts {
		if seen[p] {
			continue
		}
		seen[p] = true
		have, err := s.CAS.Has(p)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, protocol.ErrorBody{Error: err.Error()})
			return
		}
		if !have {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusConflict, protocol.ErrorBody{Error: "missing blobs", Missing: missing})
		return
	}
	exists, err := s.CAS.Concat(digest, parts, protocol.MaxBlobBytes)
	s.finishPut(w, digest, exists, true, err)
}

func (s *Server) finishPut(w http.ResponseWriter, digest string, exists, complete bool, err error) {
	if err != nil {
		// A wrong digest, a bad range, or an oversize body is the client's
		// mistake. mkdir, rename, and other IO are the lake's.
		code := http.StatusInternalServerError
		if errors.Is(err, cas.ErrRejected) {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, protocol.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, protocol.PutResponse{Exists: exists, SHA256: digest, Complete: complete})
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	var m protocol.Manifest
	if err := decodeJSON(w, r, &m); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	if err := validateManifest(&m); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	decisions, err := s.resolve(r.Context(), &m)
	if err != nil {
		code, body := manifestStatus(err)
		writeJSON(w, code, body)
		return
	}
	ack, err := s.Catalog.Ingest(r.Context(), m, s.now(), decisions, s.CAS)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ack)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

func isJSON(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	ct, _, _ = strings.Cut(ct, ";")
	return strings.TrimSpace(ct) == "application/json"
}

func normalizeDigests(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("chunk_sha256s is empty")
	}
	out := make([]string, len(in))
	for i, d := range in {
		d = strings.ToLower(strings.TrimSpace(d))
		if !protocol.ValidDigest(d) {
			return nil, fmt.Errorf("invalid chunk digest")
		}
		out[i] = d
	}
	return out, nil
}

// parseContentRange reads "bytes start-end/total". end is inclusive.
func parseContentRange(v string) (start, end, total int64, err error) {
	v = strings.TrimSpace(v)
	rest, ok := strings.CutPrefix(v, "bytes ")
	if !ok {
		rest, ok = strings.CutPrefix(v, "bytes=")
	}
	if !ok {
		return 0, 0, 0, fmt.Errorf("content-range must be bytes start-end/total")
	}
	rest = strings.TrimSpace(rest)
	span, totalText, ok := strings.Cut(rest, "/")
	if !ok || totalText == "" || totalText == "*" {
		return 0, 0, 0, fmt.Errorf("content-range must include the total size")
	}
	startText, endText, ok := strings.Cut(span, "-")
	if !ok {
		return 0, 0, 0, fmt.Errorf("content-range must be bytes start-end/total")
	}
	start, err = parseNonNeg(strings.TrimSpace(startText))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("content-range start: %w", err)
	}
	end, err = parseNonNeg(strings.TrimSpace(endText))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("content-range end: %w", err)
	}
	total, err = parseNonNeg(strings.TrimSpace(totalText))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("content-range total: %w", err)
	}
	if end < start || total == 0 || end >= total {
		return 0, 0, 0, fmt.Errorf("content-range %d-%d/%d is outside the object", start, end, total)
	}
	return start, end, total, nil
}

func parseNonNeg(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		d := int64(c - '0')
		if n > (1<<63-1-d)/10 {
			return 0, fmt.Errorf("overflow")
		}
		n = n*10 + d
	}
	return n, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
