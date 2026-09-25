package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"terva.sh/lampi/internal/protocol"
)

func postManifestCode(t *testing.T, h http.Handler, m protocol.Manifest) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/manifests", bytes.NewReader(mustJSON(t, m)))
	req.Header.Set("Authorization", "Bearer sekret")
	h.ServeHTTP(rr, req)
	return rr
}

func TestRetriedTailIsUnchanged(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	prefix := []byte("{\"type\":\"meta\"}\n")
	tail := []byte("{\"type\":\"message\"}\n")
	full := append(append([]byte{}, prefix...), tail...)
	prefixSHA := putRaw(t, h, prefix)
	fullSHA := sha256Hex(full)
	tailSHA := putRaw(t, h, tail)
	postManifest(t, h, manifest("machine-a", "sid", prefix, prefixSHA, 0, prefixSHA))

	grown := manifest("machine-a", "sid", full, fullSHA, int64(len(prefix)), tailSHA)
	first := postManifest(t, h, grown)
	if first.Relation != protocol.RelationGrownFrom {
		t.Fatalf("tail: %+v", first)
	}
	before := countBlobs(t, s)

	// The ACK was lost. The client sends the same tail manifest again.
	again := postManifest(t, h, grown)
	if again.Relation != protocol.RelationUnchanged || again.SessionUID != first.SessionUID ||
		again.ArtifactIDs[0] != first.ArtifactIDs[0] || again.HeadSHA256 != fullSHA || again.HeadSize != int64(len(full)) {
		t.Fatalf("retried tail: %+v", again)
	}
	if countBlobs(t, s) != before {
		t.Fatal("retried tail stored a blob")
	}

	// The same digest with a size that is not the stored file is the
	// client's mistake, not a prefix mismatch.
	bad := grown
	bad.Artifacts = append([]protocol.Artifact(nil), grown.Artifacts...)
	bad.Artifacts[0].Size = int64(len(full)) + 1
	if rr := postManifestCode(t, h, bad); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "size") {
		t.Fatalf("retried tail with a wrong size: %d %s", rr.Code, rr.Body)
	}
}

func TestManifestRefusesBadSizeHarnessKind(t *testing.T) {
	s := openServer(t)
	h := s.Handler()
	body := []byte("{\"type\":\"meta\"}\n")
	sum := putRaw(t, h, body)

	for _, tc := range []struct {
		name string
		edit func(*protocol.Manifest)
		want string
	}{
		{"negative size", func(m *protocol.Manifest) { m.Artifacts[0].Size = -5 }, "size -5"},
		{"short size", func(m *protocol.Manifest) { m.Artifacts[0].Size = int64(len(body)) - 1 }, "does not match stored bytes"},
		{"long size", func(m *protocol.Manifest) { m.Artifacts[0].Size = int64(len(body)) + 1 }, "does not match stored bytes"},
		{"zero size", func(m *protocol.Manifest) { m.Artifacts[0].Size = 0 }, "does not match stored bytes"},
		{"unknown harness", func(m *protocol.Manifest) { m.Harness = "../../etc" }, "harness"},
		{"empty harness", func(m *protocol.Manifest) { m.Harness = "" }, "harness"},
		{"unknown kind", func(m *protocol.Manifest) { m.Artifacts[0].Kind = "opencode_db" }, "kind"},
		{"empty kind", func(m *protocol.Manifest) { m.Artifacts[0].Kind = "" }, "kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := manifest("machine-a", "sid-"+strings.ReplaceAll(tc.name, " ", "-"), body, sum, 0, sum)
			tc.edit(&m)
			rr := postManifestCode(t, h, m)
			if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
		})
	}
	counts, err := s.Catalog.Counts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if counts.Sessions != 0 || counts.Artifacts != 0 {
		t.Fatalf("a refused manifest was stored: %+v", counts)
	}

	// Every harness and kind the protocol names is accepted.
	for h2 := range knownHarnesses {
		m := manifest("machine-a", "ok-"+h2, body, sum, 0, sum)
		m.Harness = h2
		postManifest(t, h, m)
	}
	for k := range knownKinds {
		m := manifest("machine-a", "ok-kind-"+k, body, sum, 0, sum)
		m.Artifacts[0].Kind = k
		postManifest(t, h, m)
	}
}
