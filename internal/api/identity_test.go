package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

func identityLake(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	created, err := s.EnsureIdentity(dir)
	if err != nil || !created {
		t.Fatalf("EnsureIdentity created=%v err=%v", created, err)
	}
	return s, dir
}

func TestKeysRouteIsOpenSignedAndNamesNoCatalogData(t *testing.T) {
	s, _ := identityLake(t)
	s.Allow("sekret")
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath+"?nonce=n0nce-1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("keys %d %s", rr.Code, rr.Body)
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("key list is cacheable")
	}
	var signed protocol.Signed
	if err := json.Unmarshal(rr.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	var p protocol.KeysPayload
	if err := json.Unmarshal(signed.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.LakeID != s.Identity.LakeID || p.Nonce != "n0nce-1" || len(p.Keys) != 1 || p.Keys[0].Status != identity.StatusActive {
		t.Fatalf("payload %+v", p)
	}
	pub, err := identity.ParsePublic(p.Keys[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := identity.Verify(identity.ContextKeys, &signed, pub); err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"session", "artifact", "machine"} {
		if bytes.Contains(rr.Body.Bytes(), []byte(word)) {
			t.Fatalf("key list mentions %q: %s", word, rr.Body)
		}
	}
}

func TestKeysRouteRefusesABadNonce(t *testing.T) {
	s, _ := identityLake(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath+"?nonce=a%20b", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad nonce %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath+"?nonce="+strings.Repeat("a", identity.MaxNonce+1), nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("long nonce %d", rr.Code)
	}
}

func TestKeysRouteWithoutIdentityIs404(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath, nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("no identity %d", rr.Code)
	}
}

func TestKeysRouteIsRateLimited(t *testing.T) {
	s, _ := identityLake(t)
	h := s.Handler()
	limited := 0
	for range openBurst + 5 {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath, nil))
		if rr.Code == http.StatusTooManyRequests {
			limited++
			if rr.Header().Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
		}
	}
	if limited != 5 {
		t.Fatalf("%d requests limited, want 5", limited)
	}
	// The clock moving refills the bucket.
	now := s.now().Add(time.Second)
	s.Now = func() time.Time { return now }
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("after a refill %d", rr.Code)
	}
}

func TestHelloSignsTheNonceAndStillTakesAnEmptyBody(t *testing.T) {
	s, _ := identityLake(t)
	s.Allow("sekret")
	h := s.Handler()
	post := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/hello", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer sekret")
		h.ServeHTTP(rr, req)
		return rr
	}
	rr := post(`{"nonce":"abc"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("hello %d %s", rr.Code, rr.Body)
	}
	var hr protocol.HelloResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &hr); err != nil {
		t.Fatal(err)
	}
	if hr.LakeID != s.Identity.LakeID || hr.Proof == nil {
		t.Fatalf("hello %+v", hr)
	}
	if err := identity.Verify(identity.ContextHello, hr.Proof, s.Identity.Keys[0].Pub); err != nil {
		t.Fatal(err)
	}
	var proof protocol.HelloProof
	if err := json.Unmarshal(hr.Proof.Payload, &proof); err != nil || proof.Nonce != "abc" || proof.LakeID != hr.LakeID {
		t.Fatalf("proof %+v %v", proof, err)
	}
	// What a client before this release sends, and bodies it was free to
	// send while hello ignored its body.
	for _, body := range []string{`{}`, ``, `not json`, `[1,2]`, `{"nonce":7}`, strings.Repeat("x", maxHelloBytes*2)} {
		if rr := post(body); rr.Code != http.StatusOK {
			t.Fatalf("old hello %q: %d %s", body[:min(len(body), 20)], rr.Code, rr.Body)
		}
	}
	if rr := post(`{"nonce":"a b"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad nonce %d", rr.Code)
	}
}

func TestKeysRouteBypassesTheWebHandler(t *testing.T) {
	s, _ := identityLake(t)
	s.Web = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "web", http.StatusTeapot)
	})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, protocol.KeysPath, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("keys behind web %d %s", rr.Code, rr.Body)
	}
}

func TestReopenedLakeKeepsItsIdentityAndRefusesALostOne(t *testing.T) {
	s, dir := identityLake(t)
	want := s.Identity.LakeID
	s.Close()
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if created, err := s2.EnsureIdentity(dir); err != nil || created || s2.Identity.LakeID != want {
		t.Fatalf("reopen created=%v err=%v id=%v", created, err, s2.Identity)
	}
	if err := os.Remove(identity.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.EnsureIdentity(dir); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("lost identity: %v", err)
	}
}
