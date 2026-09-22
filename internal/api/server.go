// Package api is the ingest HTTP surface: health, catalog stats, hello,
// blob check, blob put, and manifests.
//
// healthz is unauthenticated and returns no catalog data, so a process
// probe does not need the device token. Every /v1 route checks the bearer
// token when one is configured. An empty Token disables that check. The
// serve command refuses to bind a non-loopback address in that state.
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
	// Token is the expected bearer token. Empty disables the check.
	Token string
	Now   func() time.Time
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
		if s.Token == "" {
			next(w, r)
			return
		}
		if !auth.Match(r.Header.Get("Authorization"), s.Token) {
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
	exists, err := s.CAS.Put(digest, r.Body, protocol.MaxBlobBytes)
	if err != nil {
		// A wrong digest or an oversize body is the client's mistake.
		// mkdir, rename, and other IO are the lake's.
		code := http.StatusInternalServerError
		if errors.Is(err, cas.ErrRejected) {
			code = http.StatusBadRequest
		}
		writeJSON(w, code, protocol.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, protocol.PutResponse{Exists: exists, SHA256: digest})
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
