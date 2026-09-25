// Package api is the ingest HTTP surface: health, catalog stats,
// divergent_copy listing, hello, blob check, blob put, and manifests.
// A stored manifest is ACKed, then
// a worker projects it into normalized JSONL and partitioned parquet.
// A projection failure is recorded on the session and does not change
// the blob. The derived view lags the ACK until the worker finishes.
//
// healthz is unauthenticated and returns no catalog data, so a process
// probe does not need a device token. Every /v1 route checks the bearer
// token when any device hash is configured. An empty Devices set
// disables that check. The serve command refuses to bind a non-loopback
// address in that state. Tokens are stored as hashes, not the bearer.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// Normalized is the directory of derived JSONL, one file per session.
	// It is not the CAS. A normalize failure removes the session's file.
	Normalized string
	// Parquet is the hive-style partition root. Files live at
	// date=YYYY-MM-DD/harness=<harness>/<session_uid>.parquet.
	Parquet string
	// Devices are SHA-256 hashes of bearer tokens. Nil or empty disables
	// the check. The plaintext is not kept on the server.
	Devices *auth.Devices
	Now     func() time.Time
	// Log gets one line per request and normalize failures. Nil discards.
	Log *slog.Logger

	// limits, when set, replaces defaultDeadlines. Tests shorten it.
	limits *deadlines
	// active counts requests in a handler. Shutdown waits for it.
	active sync.WaitGroup

	norm        *normalizeQueue
	normalizeWG sync.WaitGroup
	pubMu       sync.Mutex
	pubs        map[string]*sessionLock
	// retryBase is the first wait before a transient normalize failure
	// is tried again. Zero is defaultRetryBase. Tests shorten it.
	retryBase time.Duration
	// beforeProject, when set, runs in the worker before Project.
	// Tests use it to show that the manifest ACK does not wait.
	beforeProject func()
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
	norm := filepath.Join(dataDir, "normalized")
	if err := os.MkdirAll(norm, 0o700); err != nil {
		cat.Close()
		return nil, err
	}
	parquetDir := filepath.Join(dataDir, "parquet")
	if err := os.MkdirAll(parquetDir, 0o700); err != nil {
		cat.Close()
		return nil, err
	}
	s := &Server{
		CAS:        store,
		Catalog:    cat,
		Normalized: norm,
		Parquet:    parquetDir,
		Now:        time.Now,
		norm:       newNormalizeQueue(),
	}
	if err := s.loadNormalizeJobs(context.Background()); err != nil {
		cat.Close()
		return nil, err
	}
	s.startNormalizeWorkers()
	return s, nil
}

// OpenReadOnly opens the lake under dataDir for a command that runs
// beside serve. The catalog is read-only, and there is no normalize
// queue and no worker: serve owns those. Project reads; StoreEvents
// and the handler's writes fail.
func OpenReadOnly(dataDir string) (*Server, error) {
	cat, err := catalog.OpenReadOnly(filepath.Join(dataDir, "catalog.db"))
	if err != nil {
		return nil, err
	}
	return &Server{
		CAS:        &cas.Store{Root: filepath.Join(dataDir, "cas")},
		Catalog:    cat,
		Normalized: filepath.Join(dataDir, "normalized"),
		Parquet:    filepath.Join(dataDir, "parquet"),
		Now:        time.Now,
	}, nil
}

// Close drains the normalize queue, stops the workers, and releases
// the catalog. The CAS is just a directory. A manifest ACK does not
// wait for projection; process exit does.
func (s *Server) Close() error {
	_, err := s.Shutdown(context.Background())
	return err
}

// Shutdown waits for requests still in a handler, drains the normalize
// queue until ctx ends, stops the workers, and releases the catalog.
// Stop the HTTP server first so no request starts. A job a worker holds
// finishes. Jobs still queued stay in catalog.normalize_jobs and the
// next Open loads them; left is how many.
func (s *Server) Shutdown(ctx context.Context) (left int, err error) {
	if s.Catalog == nil {
		return 0, nil
	}
	s.active.Wait()
	if s.norm != nil {
		_ = s.WaitNormalized(ctx)
		left = s.norm.shutdown()
		s.normalizeWG.Wait()
	}
	err = s.Catalog.Close()
	s.Catalog = nil
	return left, err
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
	mux.HandleFunc("GET /v1/conflicts", s.authed(s.conflicts))
	mux.HandleFunc("POST /v1/hello", s.authed(s.hello))
	mux.HandleFunc("POST /v1/blobs/check", s.authed(s.check))
	mux.HandleFunc("PUT /v1/blobs/{digest}", s.authed(s.put))
	mux.HandleFunc("POST /v1/manifests", s.authed(s.manifest))
	return s.serveHTTP(mux)
}

func (s *Server) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Devices == nil || s.Devices.Empty() {
			next(w, r)
			return
		}
		if !s.Devices.Match(r.Header.Get("Authorization")) {
			s.fail(w, r, http.StatusUnauthorized, errors.New("unauthorized"))
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
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.StatsResponse{
		Sessions:  n.Sessions,
		Artifacts: n.Artifacts,
		Machines:  n.Machines,
	})
}

func (s *Server) conflicts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Catalog.DivergentCopies(r.Context())
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.ConflictsResponse{Conflicts: wireConflicts(rows)})
}

func wireConflicts(rows []catalog.DivergentCopy) []protocol.DivergentCopy {
	out := make([]protocol.DivergentCopy, 0, len(rows))
	for _, row := range rows {
		out = append(out, protocol.DivergentCopy{
			SessionUID:      row.SessionUID,
			ArtifactID:      row.ArtifactID,
			Harness:         row.Harness,
			NativeSessionID: row.NativeID,
			Kind:            row.Kind,
			RelPath:         row.RelPath,
			SHA256:          row.SHA256,
			Size:            row.Size,
			HeadSHA256:      row.HeadSHA256,
			HeadSize:        row.HeadSize,
			Machines:        row.Machines,
			HeadMachines:    row.HeadMachines,
		})
	}
	return out
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
	hint := fmt.Sprintf("send at most %d digests per request", maxCheckDigests)
	if !s.decodeJSON(w, r, &digests, maxCheckBytes, hint) {
		return
	}
	if len(digests) > maxCheckDigests {
		s.fail(w, r, http.StatusRequestEntityTooLarge, fmt.Errorf("blobs/check has %d digests; %s", len(digests), hint))
		return
	}
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, d := range digests {
		d = strings.ToLower(d)
		if !protocol.ValidDigest(d) {
			s.fail(w, r, http.StatusBadRequest, errors.New("invalid digest "+d))
			return
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		ok, err := s.CAS.Has(d)
		if err != nil {
			s.fail(w, r, http.StatusInternalServerError, err)
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
		s.fail(w, r, http.StatusBadRequest, errors.New("invalid digest"))
		return
	}
	cr := r.Header.Get("Content-Range")
	if cr != "" && isJSON(r.Header.Get("Content-Type")) {
		s.fail(w, r, http.StatusBadRequest, errors.New("content-range and chunk digests are different puts"))
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
	s.finishPut(w, r, digest, exists, true, err)
}

func (s *Server) putRange(w http.ResponseWriter, r *http.Request, digest, header string) {
	start, end, total, err := parseContentRange(header)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	exists, complete, err := s.CAS.PutRange(digest, start, end, total, protocol.MaxBlobBytes, r.Body)
	s.finishPut(w, r, digest, exists, complete, err)
}

func (s *Server) putChunks(w http.ResponseWriter, r *http.Request, digest string) {
	var body struct {
		ChunkSHA256s []string `json:"chunk_sha256s"`
	}
	if !s.decodeJSON(w, r, &body, maxJSONBytes, "") {
		return
	}
	parts, err := normalizeDigests(body.ChunkSHA256s)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	ok, err := s.CAS.Has(digest)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	if ok {
		// The digest is already stored. Do not read or rewrite the chunks.
		s.finishPut(w, r, digest, true, true, nil)
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
			s.fail(w, r, http.StatusInternalServerError, err)
			return
		}
		if !have {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		note(r, errors.New("missing blobs"))
		writeJSON(w, http.StatusConflict, protocol.ErrorBody{Error: "missing blobs", Missing: missing})
		return
	}
	exists, err := s.CAS.Concat(digest, parts, protocol.MaxBlobBytes)
	s.finishPut(w, r, digest, exists, true, err)
}

func (s *Server) finishPut(w http.ResponseWriter, r *http.Request, digest string, exists, complete bool, err error) {
	if err != nil {
		// A wrong digest, a bad range, or an oversize body is the client's
		// mistake. mkdir, rename, and other IO are the lake's. A body that
		// stopped arriving is the request's; fail maps it.
		code := http.StatusInternalServerError
		if errors.Is(err, cas.ErrRejected) {
			code = http.StatusBadRequest
		}
		s.fail(w, r, code, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.PutResponse{Exists: exists, SHA256: digest, Complete: complete})
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	var m protocol.Manifest
	if !s.decodeJSON(w, r, &m, maxJSONBytes, "") {
		return
	}
	if err := validateManifest(&m); err != nil {
		s.fail(w, r, http.StatusBadRequest, err)
		return
	}
	decisions, err := s.resolve(r.Context(), &m)
	if err != nil {
		code, body := manifestStatus(err)
		if code >= 500 {
			s.fail(w, r, code, err)
			return
		}
		note(r, err)
		writeJSON(w, code, body)
		return
	}
	// validateManifest has checked what the client controls. An ingest
	// error here is the catalog's: a busy or full disk, not a bad post.
	ack, err := s.Catalog.Ingest(r.Context(), m, s.now(), decisions, s.CAS)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	// Normalization is derived and runs on the workers. The ACK does not
	// wait for it. A failure is recorded on the session later. The CAS
	// object is not written. WithoutCancel keeps the enqueue when the
	// client has already dropped the request after ingest committed.
	if err := s.enqueueNormalize(context.WithoutCancel(r.Context()), ack.SessionUID); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, ack)
}

// decodeJSON reads one JSON value of at most limit bytes into dest. On
// failure it writes the error and returns false: 413 over the cap, with
// hint appended when set, 408 when the body timed out, 400 otherwise.
func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dest any, limit int64, hint string) bool {
	defer r.Body.Close()
	body := http.MaxBytesReader(innermost(w), r.Body, limit)
	err := json.NewDecoder(body).Decode(dest)
	if err == nil {
		return true
	}
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		msg := fmt.Sprintf("request body exceeds %d bytes", tooBig.Limit)
		if hint != "" {
			msg += "; " + hint
		}
		s.fail(w, r, http.StatusRequestEntityTooLarge, errors.New(msg))
		return false
	}
	if info := infoOf(r); info != nil && info.body.err != nil {
		code, berr := bodyStatus(info.body.err)
		note(r, info.body.err)
		writeJSON(w, code, protocol.ErrorBody{Error: berr.Error()})
		return false
	}
	s.fail(w, r, http.StatusBadRequest, fmt.Errorf("invalid json: %w", err))
	return false
}

// innermost unwraps middleware writers so http.MaxBytesReader can tell
// the server to close the connection after a body over the cap.
func innermost(w http.ResponseWriter) http.ResponseWriter {
	for {
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return w
		}
		w = u.Unwrap()
	}
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
