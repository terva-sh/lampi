package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

func normalizeGen(t *testing.T, s *Server, native string) int64 {
	t.Helper()
	ctx := context.Background()
	if err := s.WaitNormalized(ctx); err != nil {
		t.Fatal(err)
	}
	uid, _, ok, err := s.Catalog.Current(ctx, protocol.HarnessTerva, native)
	if err != nil || !ok {
		t.Fatalf("current ok=%v err=%v", ok, err)
	}
	gen, _, err := s.Catalog.NormalizeGen(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	return gen
}

// A post whose every artifact is unchanged does not project the session
// again. A post that grows it does, and so does one that learns the
// project id, or one for a session whose last projection failed.
func TestUnchangedRepostDoesNotRenormalize(t *testing.T) {
	s := openServer(t)
	m := chunkedSession(t, s, 1<<20)
	postManifestOK(t, s, m)
	first := normalizeGen(t, s, "big")

	postManifestOK(t, s, m)
	if got := normalizeGen(t, s, "big"); got != first {
		t.Fatalf("unchanged re-post moved normalize_gen %d to %d", first, got)
	}

	learned := m
	learned.Project = protocol.Project{CWD: "/work/app", GitRemote: "https://github.com/terva-sh/lampi", GitRoot: strings.Repeat("a", 40)}
	postManifestOK(t, s, learned)
	second := normalizeGen(t, s, "big")
	if second == first {
		t.Fatal("a newly learned project id did not project again")
	}

	ctx := context.Background()
	uid, _, _, _ := s.Catalog.Current(ctx, protocol.HarnessTerva, "big")
	if err := s.Catalog.SetNormalizeError(ctx, uid, "earlier failure"); err != nil {
		t.Fatal(err)
	}
	postManifestOK(t, s, learned)
	if got := normalizeGen(t, s, "big"); got == second {
		t.Fatal("a session whose projection failed was not projected again")
	}
}

// An unchanged re-post of a file past the blob cap reads none of it:
// the lake used to hold the stored file and the posted one in memory.
func TestUnchangedRepostDoesNotReadTheFile(t *testing.T) {
	s := openServer(t)
	m := chunkedSession(t, s, int(protocol.MaxBlobBytes)+(4<<20))
	postManifestOK(t, s, m)
	if err := s.WaitNormalized(context.Background()); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	postManifestOK(t, s, m)
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 4<<20 {
		t.Fatalf("unchanged re-post of a %d byte file allocated %d bytes", m.Artifacts[0].Size, n)
	}
}

// A grown file past the cap is related to the stored one by streaming
// both: the post records grown_from and moves the head.
func TestGrownChunkedFileIsRelatedByStream(t *testing.T) {
	s := openServer(t)
	m := chunkedSession(t, s, 1<<20)
	postManifestOK(t, s, m)
	stored, err := s.CAS.Read(m.Artifacts[0].SHA256)
	if err != nil {
		t.Fatal(err)
	}
	grown := append(stored, []byte(`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"more"}]}}`+"\n")...)
	next := m
	next.Artifacts = []protocol.Artifact{{
		Kind:    protocol.KindTranscriptJSONL,
		RelPath: m.Artifacts[0].RelPath,
		Size:    int64(len(grown)),
		SHA256:  putBlob(t, s.Handler(), "", grown),
	}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, next)))
	req.Header.Set("Authorization", "Bearer sekret")
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"relation":"grown_from"`) {
		t.Fatalf("grown post %d %s", rr.Code, rr.Body)
	}
}

// Blob PUTs and manifest posts share a few slots. A request past them
// waits for one, and gets 503 with Retry-After only when its own budget
// runs out first.
func TestBusyLakeQueuesPutsAndManifests(t *testing.T) {
	s := openServer(t)
	s.heavyN = 1
	s.limits = &deadlines{json: 300 * time.Millisecond, blob: 300 * time.Millisecond, slack: time.Second}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	put := func(body string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, srv.URL+"/v1/blobs/"+shaOf(t, []byte(body)), strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer sekret")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}

	slots := s.slots()
	slots <- struct{}{}
	start := time.Now()
	resp := put("busy")
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("busy put %d %v", resp.StatusCode, resp.Header)
	}
	if waited := time.Since(start); waited < 250*time.Millisecond {
		t.Fatalf("busy put failed after %s, before its budget", waited)
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		<-slots
	}()
	if resp := put("queued"); resp.StatusCode != http.StatusOK {
		t.Fatalf("queued put %d", resp.StatusCode)
	}
	if len(slots) != 0 {
		t.Fatalf("slot not released: %d held", len(slots))
	}
}
