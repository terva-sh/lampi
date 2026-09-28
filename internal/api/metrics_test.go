package api

import (
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// TKT-01M3JV461: the metrics endpoint reports storage, queues, devices
// and request counts in a well-formed exposition, each family declared
// once.
func TestMetricsExposition(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.Allow("sekret")
	h := s.Handler()
	body := transcriptLines(`{"type":"user","message":{"role":"user","content":"pond"}}`)
	sha := putBlob(t, h, "", body)
	postManifest(t, h, protocol.Manifest{
		CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: protocol.HarnessClaude, HarnessVersion: "1", NativeSessionID: "sid-m",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/sid-m.jsonl", Size: int64(len(body)), SHA256: sha}},
	})
	if err := s.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SampleStorage(t.Context()); err != nil {
		t.Fatal(err)
	}
	m := s.MetricsHandler(MetricsInfo{Version: `v1 "quoted"`, Started: time.Unix(1700000000, 0)})
	w := httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("metrics: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
	out := w.Body.String()
	for _, want := range []string{
		`lampi_build_info{version="v1 \"quoted\""} 1`,
		"lampi_start_time_seconds 1.7e+09",
		`lampi_storage_bytes{component="cas"} `,
		`lampi_storage_files{component="cas"} 1`,
		`lampi_artifact_unique_bytes `,
		"lampi_sessions 1",
		`lampi_sessions_by_normalization{state="ready"} 1`,
		"lampi_audit_outbox_events 0",
		`lampi_device_last_contact_timestamp_seconds{device="allow"} `,
		`lampi_http_requests_total{route="manifest",code="2xx"} 1`,
		`lampi_http_requests_total{route="blob_put",code="2xx"} 1`,
		`lampi_http_request_body_bytes_total{route="blob_put"} ` + strconv.Itoa(len(body)),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	line := regexp.MustCompile(`^[a-z_]+(\{[a-z_]+="([^"\\]|\\.)*"(,[a-z_]+="([^"\\]|\\.)*")*\})? -?[0-9.e+]+$`)
	declared := map[string]int{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.HasPrefix(l, "# TYPE ") {
			declared[strings.Fields(l)[2]]++
			continue
		}
		if strings.HasPrefix(l, "# HELP ") {
			continue
		}
		if !line.MatchString(l) {
			t.Errorf("malformed line %q", l)
		}
	}
	for name, n := range declared {
		if n != 1 {
			t.Errorf("%s declared %d times", name, n)
		}
	}
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("GET", "/other", nil))
	if w.Code != 404 {
		t.Errorf("other path: %d", w.Code)
	}
	w = httptest.NewRecorder()
	m.ServeHTTP(w, httptest.NewRequest("POST", "/metrics", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Errorf("POST: %d %q", w.Code, w.Header().Get("Allow"))
	}
}

func TestRouteClass(t *testing.T) {
	for path, want := range map[string]string{
		"/healthz": routeHealth, protocol.KeysPath: routeKeys, "/v1/stats": routeStats, "/v1/hello": routeHello,
		protocol.AgentConfigPath: routeConfig, protocol.RegisterPath: routeRegister, "/v1/blobs/check": routeCheck,
		"/v1/blobs/" + strings.Repeat("a", 64): routePut, "/v1/manifests": routeManifest, "/v1/nope": routeOther,
		"/sessions/uid-1": routeWeb, "/": routeWeb,
	} {
		if got := routeClass(httptest.NewRequest("GET", path, nil)); got != want {
			t.Errorf("routeClass(%s) = %s, want %s", path, got, want)
		}
	}
}

// TKT-01M3JV461: label values use the exposition format's three
// escapes and nothing else.
func TestLabelValueEscapes(t *testing.T) {
	for in, want := range map[string]string{
		`plain`:      `plain`,
		`a"b`:        `a\"b`,
		`c:\dir`:     `c:\\dir`,
		"two\nlines": `two\nlines`,
		"tab\there":  "tab\there",
		"naïve ☃":    "naïve ☃",
	} {
		if got := labelValue(in); got != want {
			t.Errorf("labelValue(%q) = %q, want %q", in, got, want)
		}
	}
}
