package cli

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/webconfig"
)

func TestWebRequiresDeviceTokensEvenOnLoopback(t *testing.T) {
	p := filepath.Join(t.TempDir(), "web.json")
	if err := os.WriteFile(p, []byte(`{"base_url":"https://lake.example","oidc":{"issuer":"https://id.example","client_id":"lake","role_map":{"readers":"viewer"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := runServe(Env{Stderr: &out}, []string{"--addr", "127.0.0.1:0", "--data", filepath.Join(t.TempDir(), "lake"), "--web-config", p})
	if err == nil || !strings.Contains(err.Error(), "nonempty device tokens") {
		t.Fatalf("got %v", err)
	}
	if strings.Contains(out.String(), "listening") {
		t.Fatal("opened listener before validation")
	}
}

// TestStartWebIndexesPublishedSessions runs the web wiring serve uses:
// an upload is normalized, the publish hook wakes the index, and Close
// stops the index before the catalog.
func TestStartWebIndexesPublishedSessions(t *testing.T) {
	data := t.TempDir()
	lake, err := api.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: "https://id.example", ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	if err := startWeb(cfg, data, lake); err != nil {
		t.Fatal(err)
	}
	h := lake.Handler()
	body := []byte(`{"type":"meta","meta":{"id":"sid-web","cwd":"/tmp","started":"2026-09-22T16:10:00Z","version":"0.1.0"}}` + "\n" +
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"find the heron"}],"time":"2026-09-22T16:10:01Z"}}` + "\n")
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	send := func(method, path string, b []byte) {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer sekret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	send("PUT", "/v1/blobs/"+digest, body)
	m, _ := json.Marshal(protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessTerva, NativeSessionID: "sid-web",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/x/sid-web.jsonl", Size: int64(len(body)), SHA256: digest}}})
	send("POST", "/v1/manifests", m)
	db, err := sql.Open("sqlite", filepath.Join(data, recall.IndexFile)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if db.QueryRow(`SELECT COUNT(*) FROM docs WHERE content LIKE '%heron%'`).Scan(&n); n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("index did not catch up")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}
}
