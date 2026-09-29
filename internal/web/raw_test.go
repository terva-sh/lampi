package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/testidp"
	"terva.sh/lampi/internal/webconfig"
)

// rawLake is a lake with raw reads on: readers are viewers, ops are
// operators and owners are admins. auditCat, when not nil, replaces the
// catalog the audit outbox is written through.
func rawLake(t *testing.T, auditCat func(dir string) *catalog.Catalog) (*api.Server, *testidp.Server, http.Handler, string) {
	t.Helper()
	idp := testidp.New()
	t.Cleanup(idp.Close)
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lake.Close() })
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer", "ops": "operator", "owners": "admin"}}}
	cat := lake.Catalog
	if auditCat != nil {
		cat = auditCat(dir)
	}
	reg := &Registrations{
		Lake:  func() registrar.Lake { return registrar.Lake{Catalog: cat, Dir: dir} },
		Blobs: lake.CAS,
	}
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, reg, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	return lake, idp, lake.Handler(), dir
}

func signInAs(t *testing.T, idp *testidp.Server, h http.Handler, group string) *http.Cookie {
	t.Helper()
	idp.Groups = []string{group}
	c, _ := signIn(t, idp, h)
	return c
}

// storeSession puts body in the lake's blob store and ingests a session
// whose head artifact it is.
func storeSession(t *testing.T, lake *api.Server, native string, body []byte) (uid, digest string) {
	t.Helper()
	digest = putBlob(t, lake, body)
	return ingestHead(t, lake, native, digest, int64(len(body))), digest
}

func putBlob(t *testing.T, lake *api.Server, body []byte) string {
	t.Helper()
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	if _, err := lake.CAS.Put(digest, bytes.NewReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	return digest
}

// ingestHead records a session whose head artifact is digest, already
// in the store in some form.
func ingestHead(t *testing.T, lake *api.Server, native, digest string, size int64) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "codex", NativeSessionID: native, Project: protocol.Project{CWD: "/synthetic"},
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/" + native + "/rollout.jsonl", SHA256: digest, Size: size}}}
	ack, err := lake.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

func getWith(h http.Handler, path string, c *http.Cookie, header map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://lake.example"+path, nil)
	for k, v := range header {
		r.Header.Set(k, v)
	}
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TKT-01M3NKY2V3: only admins see the Raw tab and reach the raw
// routes. Operators and viewers get 404.
func TestRawIsAdminOnly(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, digest := storeSession(t, lake, "raw-a", []byte(`{"synthetic":1}`+"\n"))
	file := rawPath(uid) + "/" + digest
	for _, group := range []string{"readers", "ops"} {
		c := signInAs(t, idp, h, group)
		for _, p := range []string{rawPath(uid), file} {
			if w := get(h, p, c); w.Code != 404 {
				t.Errorf("%s %s: %d, want 404", group, p, w.Code)
			}
		}
		if strings.Contains(get(h, "/sessions/"+uid, c).Body.String(), rawPath(uid)) {
			t.Errorf("%s sees the Raw tab", group)
		}
	}
	if w := get(h, file, nil); w.Code == 200 {
		t.Fatal("anonymous raw read")
	}
	admin := signInAs(t, idp, h, "owners")
	if !strings.Contains(get(h, "/sessions/"+uid, admin).Body.String(), `href="`+rawPath(uid)+`"`) {
		t.Fatal("admin has no Raw tab")
	}
	page := get(h, rawPath(uid), admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), digest) || !strings.Contains(page.Body.String(), "not redacted") || !strings.Contains(page.Body.String(), "stops at 8 MiB") {
		t.Fatalf("raw page %d", page.Code)
	}
}

// With no blob store wired, the routes and the tab do not exist, even
// for an admin.
func TestRawOffWithoutBlobs(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, _ := storeSession(t, lake, "raw-off", []byte("x"))
	// Rebuild the web handler with registrations but no blob store.
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"owners": "admin"}}}
	var err error
	lake.Web, err = New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, &Registrations{Lake: func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog} }}, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	h = lake.Handler()
	admin := signInAs(t, idp, h, "owners")
	if w := get(h, rawPath(uid), admin); w.Code != 404 {
		t.Fatalf("raw page without blobs: %d", w.Code)
	}
	if strings.Contains(get(h, "/sessions/"+uid, admin).Body.String(), rawPath(uid)) {
		t.Fatal("Raw tab without blobs")
	}
}

// A download is the stored bytes as an attachment, never inline, and
// its read is in the audit log without the content.
func TestRawDownloadIsAnAuditedAttachment(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	body := []byte(`{"type":"message","text":"synthetic-raw-marker"}` + "\n")
	uid, digest := storeSession(t, lake, "raw-b", body)
	admin := signInAs(t, idp, h, "owners")
	w := get(h, rawPath(uid)+"/"+digest, admin)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatalf("download %d %q", w.Code, w.Body.String())
	}
	hd := w.Header()
	if hd.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(hd.Get("Content-Disposition"), "attachment;") ||
		!strings.Contains(hd.Get("Content-Disposition"), "rollout.jsonl") || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Cache-Control") != "no-store" || hd.Get(RawTruncatedHeader) != "" {
		t.Fatalf("headers %v", hd)
	}
	log, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	line := string(log)
	if !strings.Contains(line, `"kind":"artifact.read"`) || !strings.Contains(line, "session="+uid) || !strings.Contains(line, "sha256="+digest) ||
		!strings.Contains(line, `"actor":"web:synthetic-viewer`) {
		t.Fatalf("audit %s", line)
	}
	if strings.Contains(line, "synthetic-raw-marker") {
		t.Fatal("audit line holds content")
	}
}

// A digest the session does not link to is 404, even when the store
// holds it, and so is one that is nowhere.
func TestRawRefusesForeignDigests(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, _ := storeSession(t, lake, "raw-mine", []byte("mine\n"))
	_, other := storeSession(t, lake, "raw-theirs", []byte("theirs\n"))
	admin := signInAs(t, idp, h, "owners")
	for _, d := range []string{other, strings.Repeat("c", 64), "..%2F..%2Fcatalog.db"} {
		if w := get(h, rawPath(uid)+"/"+d, admin); w.Code != 404 {
			t.Errorf("digest %q: %d, want 404", d, w.Code)
		}
	}
	if w := get(h, rawPath("01NOSUCHSESSION")+"/"+other, admin); w.Code != 404 {
		t.Errorf("missing session: %d", w.Code)
	}
}

// Compressed objects and prefix records read back as the bytes that
// were uploaded.
func TestRawReadsEveryStoredForm(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	long := bytes.Repeat([]byte(`{"line":"synthetic compressible text"}`+"\n"), 4000)
	uidZ, digestZ := storeSession(t, lake, "raw-zstd", long)
	if _, _, err := lake.CAS.Reencode(digestZ); err != nil {
		t.Fatal(err)
	}
	if _, compressed, err := lake.CAS.ObjectPath(digestZ); err != nil || !compressed {
		t.Fatalf("not compressed: %v", err)
	}
	prefix := long[:len(long)/2]
	uidP, digestP := storeSession(t, lake, "raw-prefix", prefix)
	if _, err := lake.CAS.Fold(digestP, digestZ, int64(len(prefix))); err != nil {
		t.Fatal(err)
	}
	if ok, _ := lake.CAS.Has(digestP); ok {
		t.Fatal("prefix still an object")
	}
	admin := signInAs(t, idp, h, "owners")
	for _, c := range []struct {
		uid, digest string
		want        []byte
	}{{uidZ, digestZ, long}, {uidP, digestP, prefix}} {
		w := get(h, rawPath(c.uid)+"/"+c.digest, admin)
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), c.want) {
			t.Errorf("%s: %d, %d bytes, want %d", c.digest[:8], w.Code, w.Body.Len(), len(c.want))
		}
	}
}

// A read stops at RawReadCap and says so; Range fetches the rest.
func TestRawCapAndRange(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	body := make([]byte, RawReadCap+1000)
	for i := range body {
		body[i] = byte('a' + i%26)
	}
	uid, digest := storeSession(t, lake, "raw-big", body)
	admin := signInAs(t, idp, h, "owners")
	file := rawPath(uid) + "/" + digest
	size := strconv.Itoa(len(body))

	w := get(h, file, admin)
	if w.Code != 206 || w.Body.Len() != int(RawReadCap) || w.Header().Get(RawTruncatedHeader) != size ||
		w.Header().Get("Content-Range") != "bytes 0-"+strconv.FormatInt(RawReadCap-1, 10)+"/"+size {
		t.Fatalf("capped read %d %d %v", w.Code, w.Body.Len(), w.Header())
	}
	rest := getWith(h, file, admin, map[string]string{"Range": "bytes=" + strconv.FormatInt(RawReadCap, 10) + "-"})
	if rest.Code != 206 || !bytes.Equal(rest.Body.Bytes(), body[RawReadCap:]) || rest.Header().Get(RawTruncatedHeader) != "" {
		t.Fatalf("rest %d %d %v", rest.Code, rest.Body.Len(), rest.Header())
	}
	tail := getWith(h, file, admin, map[string]string{"Range": "bytes=-10"})
	if tail.Code != 206 || !bytes.Equal(tail.Body.Bytes(), body[len(body)-10:]) {
		t.Fatalf("suffix %d %q", tail.Code, tail.Body.String())
	}
	mid := getWith(h, file, admin, map[string]string{"Range": "bytes=5-9"})
	if mid.Code != 206 || mid.Body.String() != string(body[5:10]) || mid.Header().Get("Content-Range") != "bytes 5-9/"+size {
		t.Fatalf("mid %d %q %v", mid.Code, mid.Body.String(), mid.Header())
	}
	for _, bad := range []string{"bytes=" + size + "-", "bytes=0-1,4-5", "items=0-1", "bytes=9-5", "bytes=-0"} {
		if w := getWith(h, file, admin, map[string]string{"Range": bad}); w.Code != 416 || w.Header().Get("Content-Range") != "bytes */"+size {
			t.Errorf("range %q: %d %v", bad, w.Code, w.Header())
		}
	}
}

// review 1323: two Range fields are two ranges, and are refused.
func TestRawRefusesRepeatedRangeFields(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, digest := storeSession(t, lake, "raw-ranges", []byte("0123456789"))
	admin := signInAs(t, idp, h, "owners")
	r := httptest.NewRequest("GET", "https://lake.example"+rawPath(uid)+"/"+digest, nil)
	r.Header.Add("Range", "bytes=0-1")
	r.Header.Add("Range", "bytes=4-5")
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 416 || w.Header().Get("Content-Range") != "bytes */10" {
		t.Fatalf("two Range fields: %d %v", w.Code, w.Header())
	}
}

// review 1323: a read that fails before any byte is read records no
// audit event, and a HEAD, which sends no bytes, records none either.
func TestRawAuditsOnlyBytesRead(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	first, second := []byte("synthetic first chunk\n"), []byte("synthetic second chunk\n")
	whole := append(append([]byte{}, first...), second...)
	sum := sha256.Sum256(whole)
	digest := hex.EncodeToString(sum[:])
	a, b := putBlob(t, lake, first), putBlob(t, lake, second)
	if _, err := lake.CAS.BindLogical(digest, []string{a, b}, []int64{int64(len(first)), int64(len(second))}); err != nil {
		t.Fatal(err)
	}
	uid := ingestHead(t, lake, "raw-gone", digest, int64(len(whole)))
	admin := signInAs(t, idp, h, "owners")
	r := httptest.NewRequest("HEAD", "https://lake.example"+rawPath(uid)+"/"+digest, nil)
	r.AddCookie(admin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") != strconv.Itoa(len(whole)) {
		t.Fatalf("HEAD %d %d %v", w.Code, w.Body.Len(), w.Header())
	}
	// Lose the second chunk: the logical file still has a size, from its
	// chunk list, but its bytes no longer read back.
	if err := lake.CAS.Remove(b); err != nil {
		t.Fatal(err)
	}
	if n, err := lake.CAS.Size(digest); err != nil || n != int64(len(whole)) {
		t.Fatalf("size after losing a chunk %d %v; the test needs it", n, err)
	}
	w = get(h, rawPath(uid)+"/"+digest, admin)
	if w.Code != 500 || w.Header().Get("Content-Disposition") != "" || !strings.Contains(w.Body.String(), "read_failed") || strings.Contains(w.Body.String(), "synthetic") {
		t.Fatalf("damaged object: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if log, err := os.ReadFile(audit.Path(dir)); err == nil && strings.Contains(string(log), "artifact.read") {
		t.Fatalf("audit names a read that did not happen: %s", log)
	}
}

// A read whose audit line cannot be queued sends no bytes.
func TestRawRefusedWhenAuditFails(t *testing.T) {
	lake, idp, h, _ := rawLake(t, func(dir string) *catalog.Catalog {
		ro, err := catalog.OpenReadOnly(filepath.Join(dir, "catalog.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ro.Close() })
		return ro
	})
	uid, digest := storeSession(t, lake, "raw-audit", []byte("synthetic-secret-bytes\n"))
	admin := signInAs(t, idp, h, "owners")
	w := get(h, rawPath(uid)+"/"+digest, admin)
	if w.Code != 500 || strings.Contains(w.Body.String(), "synthetic-secret-bytes") || !strings.Contains(w.Body.String(), "audit_failed") {
		t.Fatalf("read with a failing audit: %d %q", w.Code, w.Body.String())
	}
}

func TestParseRange(t *testing.T) {
	for _, c := range []struct {
		h          string
		size       int64
		start, end int64
		ranged, ok bool
	}{
		{"", 10, 0, 10, false, true},
		{"", 0, 0, 0, false, true},
		{"bytes=0-", 10, 0, 10, true, true},
		{"bytes=2-4", 10, 2, 5, true, true},
		{"bytes=2-400", 10, 2, 10, true, true},
		{"bytes=-3", 10, 7, 10, true, true},
		{"bytes=-30", 10, 0, 10, true, true},
		{"bytes=10-", 10, 0, 0, false, false},
		{"bytes=0-", 0, 0, 0, false, false},
		{"bytes=a-b", 10, 0, 0, false, false},
		{"bytes=5", 10, 0, 0, false, false},
	} {
		s, e, r, ok := parseRange(c.h, c.size)
		if ok != c.ok || (ok && (s != c.start || e != c.end || r != c.ranged)) {
			t.Errorf("%q/%d: %d %d %v %v", c.h, c.size, s, e, r, ok)
		}
	}
}
