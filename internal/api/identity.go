package api

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

// catalogRecorder records the lake id in the catalog for identity.Ensure.
type catalogRecorder struct{ c *catalog.Catalog }

func (r catalogRecorder) LakeID() (string, error) { return r.c.LakeID(context.Background()) }
func (r catalogRecorder) RecordLakeID(id string) error {
	return r.c.RecordLakeID(context.Background(), id)
}

// EnsureIdentity loads the lake's identity from dataDir, or makes one
// when the catalog has never recorded a lake id, and sets s.Identity.
// created is true when this call made it.
func (s *Server) EnsureIdentity(dataDir string) (created bool, err error) {
	id, created, err := identity.Ensure(dataDir, catalogRecorder{s.Catalog}, rand.Reader, s.now())
	if err != nil {
		return false, err
	}
	s.Identity = id
	return created, nil
}

// keys answers GET protocol.KeysPath without a token. It returns the
// lake id and public keys, signed over the caller's nonce, and no
// catalog data.
func (s *Server) keys(w http.ResponseWriter, r *http.Request) {
	if !s.openLimit().allow(s.now()) {
		w.Header().Set("Retry-After", "1")
		s.fail(w, r, http.StatusTooManyRequests, errors.New("too many requests; try again"))
		return
	}
	if s.Identity == nil {
		s.fail(w, r, http.StatusNotFound, errors.New("this lake has no identity"))
		return
	}
	nonce := r.URL.Query().Get("nonce")
	if !identity.ValidNonce(nonce) {
		s.fail(w, r, http.StatusBadRequest, errors.New("nonce is at most 128 characters of base64url or hex"))
		return
	}
	now := s.now().UTC()
	signed, err := s.Identity.Sign(identity.ContextKeys, protocol.KeysPayload{
		LakeID:   s.Identity.LakeID,
		Nonce:    nonce,
		IssuedAt: now,
		Keys:     s.Identity.Public(),
	}, now)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, signed)
}

// helloProof signs the hello nonce when the lake has an identity.
func (s *Server) helloProof(nonce string, now time.Time) (string, *protocol.Signed, error) {
	if s.Identity == nil {
		return "", nil, nil
	}
	signed, err := s.Identity.Sign(identity.ContextHello, protocol.HelloProof{
		LakeID:     s.Identity.LakeID,
		Nonce:      nonce,
		ServerTime: now,
	}, now)
	if err != nil {
		return "", nil, err
	}
	return s.Identity.LakeID, signed, nil
}

// Open routes, the ones that need no token, share one rate limit. Behind
// a proxy every caller has the proxy's address, so the limit is global,
// not per address. A registering agent makes a handful of calls.
const (
	openRate  = 5  // requests per second, refilled
	openBurst = 20 // requests at once
)

type rateLimit struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64
	burst  float64
}

func (s *Server) openLimit() *rateLimit {
	s.openOnce.Do(func() {
		if s.open == nil {
			s.open = &rateLimit{rate: openRate, burst: openBurst, tokens: openBurst}
		}
	})
	return s.open
}

func (l *rateLimit) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.last.IsZero() {
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
