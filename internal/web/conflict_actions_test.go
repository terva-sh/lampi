package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"errors"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/webconfig"
)

// A divergence is located by byte and line, a file that ends first is
// named, equal digests are the same file, and the limit stops the read
// (TKT-01M3PTMWHR).
func TestWhereTheyPart(t *testing.T) {
	lake, _, _, _ := rawLake(t, nil)
	put := func(s string) string { return putBlob(t, lake, []byte(s)) }
	long := strings.Repeat("x", 100<<10)
	for _, c := range []struct {
		name, copy, head string
		limit            int64
		want             partView
	}{
		{"second line", "one\ntwo\nthree\n", "one\ntwx\nthree\n", partCompareLimit, partView{Offset: 6, Line: 2}},
		{"first byte", "abc", "xbc", partCompareLimit, partView{Offset: 0, Line: 1}},
		{"copy ends", "one\ntwo\n", "one\ntwo\nthree\n", partCompareLimit, partView{Offset: 8, Line: 3, Ends: "copy"}},
		{"head ends", "one\ntwo\nthree\n", "one\n", partCompareLimit, partView{Offset: 4, Line: 2, Ends: "head"}},
		{"across buffers", long + "\na", long + "\nb", partCompareLimit, partView{Offset: 100<<10 + 1, Line: 2}},
		{"past the limit", long + "a", long + "b", 64 << 10, partView{Offset: 64 << 10, Line: 1, Beyond: true}},
	} {
		got, err := whereTheyPart(context.Background(), lake.CAS, put(c.copy), put(c.head), c.limit)
		if err != nil {
			t.Fatal(c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	same := put("same")
	if got, err := whereTheyPart(context.Background(), lake.CAS, same, same, partCompareLimit); err != nil || !got.Same {
		t.Errorf("same digest: %+v %v", got, err)
	}
}

// forkSession stores a session whose head is head and a divergent copy
// of it, and returns the session and the copy's artifact id.
func forkSession(t *testing.T, lake *api.Server, native, head, fork string) (string, string) {
	t.Helper()
	uid, _ := storeSession(t, lake, native, []byte(head))
	digest := putBlob(t, lake, []byte(fork))
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-b", Harness: "codex", NativeSessionID: native,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/" + native + "/rollout.jsonl", SHA256: digest, Size: int64(len(fork))}}}
	ack, err := lake.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return uid, ack.ArtifactIDs[0]
}

func TestConflictPageShowsWhereTheyPart(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, id := forkSession(t, lake, "forked", "one\ntwo\nthree\n", "one\ntwX\n")
	viewer := signInAs(t, idp, h, "readers")
	w := get(h, conflictURL(id), viewer)
	if w.Code != 200 {
		t.Fatalf("page %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"They part at byte 6, on line 2.", ">shorter<", "machine-a", "machine-b", uid} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// A viewer sees no action and cannot post one; neither sees raw
	// links, which are an admin's.
	for _, no := range []string{"Keep the head", "Download raw", "/keep-head"} {
		if strings.Contains(body, no) {
			t.Errorf("viewer page has %q", no)
		}
	}
	if w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrfOf(t, h, viewer)}}, viewer); w.Code != 404 {
		t.Errorf("viewer keep-head: %d", w.Code)
	}
	if list := get(h, "/conflicts", viewer).Body.String(); !strings.Contains(list, `href="`+conflictURL(id)+`"`) {
		t.Error("the list does not link the conflict")
	}
	for _, path := range []string{conflictURL("nope"), conflictURL(id) + "?x=1"} {
		if w := get(h, path, viewer); w.Code != 404 && w.Code != 400 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	if w := get(h, conflictURL(id), nil); w.Code != 303 {
		t.Errorf("signed out: %d", w.Code)
	}
	admin := signInAs(t, idp, h, "owners")
	if body := get(h, conflictURL(id), admin).Body.String(); strings.Count(body, "Download raw") != 2 {
		t.Error("admin page lacks both raw links")
	}
}

func TestOperatorKeepsTheHeadAndReopens(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	_, id := forkSession(t, lake, "forked", "one\ntwo\n", "one\nzzz\n")
	op := signInAs(t, idp, h, "ops")
	csrf := csrfOf(t, h, op)
	page := get(h, conflictURL(id), op).Body.String()
	if !strings.Contains(page, "Keep the head") || strings.Contains(page, "Reopen") {
		t.Fatal("operator page lacks the keep-head form")
	}
	if w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"note": {"checked"}}, op); w.Code != 403 {
		t.Errorf("without csrf: %d", w.Code)
	}
	for _, note := range []string{"bad\nnote", "checked\n", "tab\there", strings.Repeat("x", maxConflictNote+1)} {
		if w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrf}, "note": {note}}, op); w.Code != 400 || !strings.Contains(w.Body.String(), "one line") {
			t.Errorf("note %.20q: %d", note, w.Code)
		}
	}
	// 500 characters is the limit, not 500 bytes.
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", `{"note":"`+strings.Repeat("é", maxConflictNote)+`"}`, op, map[string]string{CSRFHeader: csrf}); w.Code != 200 {
		t.Fatalf("a 500-character note: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/reopen", "", op, map[string]string{CSRFHeader: csrf}); w.Code != 200 {
		t.Fatalf("reopen: %d", w.Code)
	}
	w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrf}, "note": {"  same session, other laptop "}}, op)
	if w.Code != 303 || w.Header().Get("Location") != conflictURL(id) {
		t.Fatalf("keep-head: %d %s", w.Code, w.Header().Get("Location"))
	}
	page = get(h, conflictURL(id), op).Body.String()
	for _, want := range []string{"Head kept", "same session, other laptop", "Reopen"} {
		if !strings.Contains(page, want) {
			t.Errorf("after keep-head the page lacks %q", want)
		}
	}
	if w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrf}}, op); w.Code != 409 || !strings.Contains(w.Body.String(), "resolved this conflict already") {
		t.Errorf("second keep-head: %d", w.Code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if log := string(raw); !strings.Contains(log, `"kind":"conflict.resolved"`) || !strings.Contains(log, "resolution=kept_head") || !strings.Contains(log, `"actor":"web:`) {
		t.Errorf("audit log after keep-head:\n%s", log)
	}

	if w := postForm(h, conflictURL(id)+"/reopen", url.Values{"csrf": {csrf}}, op); w.Code != 303 {
		t.Fatalf("reopen: %d", w.Code)
	}
	if page = get(h, conflictURL(id), op).Body.String(); !strings.Contains(page, "Keep the head") {
		t.Error("reopened conflict lacks the keep-head form")
	}
	if raw, _ = os.ReadFile(filepath.Join(dir, audit.FileName)); !strings.Contains(string(raw), `"kind":"conflict.reopened"`) {
		t.Error("reopen was not audited")
	}
	if w := postForm(h, conflictURL(id)+"/reopen", url.Values{"csrf": {csrf}}, op); w.Code != 409 {
		t.Errorf("reopening an open conflict: %d", w.Code)
	}
	if w := postForm(h, conflictURL(id)+"/merge", url.Values{"csrf": {csrf}}, op); w.Code != 404 {
		t.Errorf("unknown action: %d", w.Code)
	}
}

func TestConflictAPI(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	_, id := forkSession(t, lake, "forked", "one\ntwo\n", "one\n")
	viewer := signInAs(t, idp, h, "readers")
	w := get(h, "/api/web/v1/conflicts/"+id, viewer)
	var got struct {
		Conflict conflictJSON `json:"conflict"`
		Error    string       `json:"error"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("get: %d %s", w.Code, w.Body)
	}
	if got.Conflict.ArtifactID != id || got.Conflict.Part == nil || got.Conflict.Part.Ends != "copy" || got.Conflict.Part.Offset != 4 || got.Conflict.Resolution != nil {
		t.Errorf("conflict %+v part %+v", got.Conflict, got.Conflict.Part)
	}
	if w := get(h, "/api/web/v1/conflicts/nope", viewer); w.Code != 404 {
		t.Errorf("missing: %d", w.Code)
	}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", "", viewer, map[string]string{CSRFHeader: csrfOf(t, h, viewer)}); w.Code != 404 {
		t.Errorf("viewer post: %d", w.Code)
	}

	op := signInAs(t, idp, h, "ops")
	hdr := map[string]string{CSRFHeader: csrfOf(t, h, op)}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", "", op, nil); w.Code != 403 {
		t.Errorf("without csrf: %d", w.Code)
	}
	for _, body := range []string{`{"note":"x","extra":1}`, `null`, `[]`, `"x"`, `{} {}`} {
		if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", body, op, hdr); w.Code != 400 {
			t.Errorf("keep-head with %s: %d", body, w.Code)
		}
		if w := post(h, "/api/web/v1/conflicts/"+id+"/reopen", body, op, hdr); w.Code != 400 {
			t.Errorf("reopen with %s: %d", body, w.Code)
		}
	}
	for _, body := range []string{`{"note":"x"}`, `{"note":""}`, `{"note":null}`} {
		if w := post(h, "/api/web/v1/conflicts/"+id+"/reopen", body, op, hdr); w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_note") {
			t.Errorf("reopen with %s: %d %s", body, w.Code, w.Body)
		}
	}
	w = post(h, "/api/web/v1/conflicts/"+id+"/keep-head", `{"note":"ok"}`, op, hdr)
	got = struct {
		Conflict conflictJSON `json:"conflict"`
		Error    string       `json:"error"`
	}{}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Conflict.Resolution == nil || got.Conflict.Resolution.Resolution != catalog.ResolutionKeptHead || got.Conflict.Resolution.Note != "ok" {
		t.Fatalf("keep-head: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", "", op, hdr); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"error":"already_resolved"`) {
		t.Errorf("again: %d %s", w.Code, w.Body)
	}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/reopen", "", op, hdr); w.Code != 200 {
		t.Errorf("reopen: %d %s", w.Code, w.Body)
	}
}

// A server with no blob store says it cannot compare, rather than that a
// read failed.
func TestConflictPageWithoutABlobStore(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	_, id := seedConflict(t, lake.Catalog, "forked")
	cookie, _ := signIn(t, idp, h)
	body := get(h, conflictURL(id), cookie).Body.String()
	if !strings.Contains(body, "does not read stored bytes") || strings.Contains(body, "could not read both files") {
		t.Error("the page blames a read failure")
	}
}

// A lake whose store cannot read one side says the read failed.
func TestConflictPageWhenAReadFails(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	storeSession(t, lake, "forked", []byte("one\n"))
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-b", Harness: "codex", NativeSessionID: "forked",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "sessions/forked/rollout.jsonl", SHA256: strings.Repeat("d", 64), Size: 9}}}
	ack, err := lake.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := get(h, conflictURL(ack.ArtifactIDs[0]), signInAs(t, idp, h, "readers")).Body.String()
	if !strings.Contains(body, "could not read both files") || strings.Contains(body, "does not read stored bytes") {
		t.Error("a failed read is not reported as one")
	}
}

func TestConflictPageNamesTheFirstByte(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	_, id := forkSession(t, lake, "forked", "abc\n", "xbc\n")
	if body := get(h, conflictURL(id), signInAs(t, idp, h, "readers")).Body.String(); !strings.Contains(body, "at byte 0 on line 1") {
		t.Error("the first-byte case does not name the offset and line")
	}
}

// An operator makes a copy the head from its page. The form carries the
// head the page showed, so a head that moved since is refused
// (TKT-01M3PTMWM9).
func TestOperatorMakesACopyTheHead(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	uid, id := forkSession(t, lake, "forked", "one\ntwo\n", "summary\n")
	viewer := signInAs(t, idp, h, "readers")
	if strings.Contains(get(h, conflictURL(id), viewer).Body.String(), "Make this the head") {
		t.Error("a viewer sees make-head")
	}
	op := signInAs(t, idp, h, "ops")
	csrf := csrfOf(t, h, op)
	head := putBlob(t, lake, []byte("one\ntwo\n"))
	if !strings.Contains(get(h, conflictURL(id), op).Body.String(), `name="head" value="`+head+`"`) {
		t.Fatal("the make-head form does not carry the head")
	}
	stale := strings.Repeat("0", 64)
	if w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrf}, "head": {stale}}, op); w.Code != 409 || !strings.Contains(w.Body.String(), "changed since this page was loaded") {
		t.Errorf("stale head: %d", w.Code)
	}
	if w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrf}}, op); w.Code != 400 {
		t.Errorf("no head: %d", w.Code)
	}
	w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrf}, "head": {head}, "note": {"compacted"}}, op)
	if w.Code != 303 {
		t.Fatalf("make-head: %d %s", w.Code, w.Body)
	}
	sum, err := lake.Catalog.DashboardSession(t.Context(), catalog.AllBays(), uid)
	if err != nil || sum.HeadSHA256 != putBlob(t, lake, []byte("summary\n")) {
		t.Fatalf("head %s %v", sum.HeadSHA256, err)
	}
	page := get(h, conflictURL(id), op).Body.String()
	for _, want := range []string{"Made the head", "This copy is the session's head now.", "There is nothing to settle."} {
		if !strings.Contains(page, want) {
			t.Errorf("after make-head the page lacks %q", want)
		}
	}
	if strings.Contains(page, ">Reopen<") {
		t.Error("the head can be reopened")
	}
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The job the change queued ran in this process, not at the next
	// start.
	if jobs, err := lake.Catalog.ListNormalizeJobs(t.Context()); err != nil || len(jobs) != 0 {
		t.Errorf("normalize jobs left: %+v %v", jobs, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, audit.FileName))
	if err != nil || !strings.Contains(string(raw), `"kind":"conflict.head_changed"`) {
		t.Errorf("audit log: %v\n%s", err, raw)
	}

	// The API refuses a head digest on other actions.
	hdr := map[string]string{CSRFHeader: csrf}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", `{"head":"`+head+`"}`, op, hdr); w.Code != 400 {
		t.Errorf("keep-head with a head: %d", w.Code)
	}
	_, id2 := forkSession(t, lake, "second", "a\nb\n", "c\n")
	for _, body := range []string{``, `{"head":null}`, `{"head":7}`, `{"head":"short"}`} {
		if w := post(h, "/api/web/v1/conflicts/"+id2+"/make-head", body, op, hdr); w.Code != 400 {
			t.Errorf("make-head with %q: %d", body, w.Code)
		}
	}
	h2 := putBlob(t, lake, []byte("a\nb\n"))
	w = post(h, "/api/web/v1/conflicts/"+id2+"/make-head", `{"head":"`+h2+`","note":"api"}`, op, hdr)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"resolution":"made_head"`) || !strings.Contains(w.Body.String(), `"same":true`) {
		t.Errorf("make-head over the API: %d %s", w.Code, w.Body)
	}
}

// An operator on a server with no blob store is not offered make-head,
// and the API names why it cannot.
func TestMakeHeadNeedsTheBlobStore(t *testing.T) {
	lake, idp, h, _ := operatorLake(t, "", "admins")
	_, id := seedConflict(t, lake.Catalog, "forked")
	op, _ := signIn(t, idp, h)
	page := get(h, conflictURL(id), op).Body.String()
	if !strings.Contains(page, "Keep the head") || strings.Contains(page, "Make this the head") {
		t.Error("make-head offered without a blob store")
	}
	w := post(h, "/api/web/v1/conflicts/"+id+"/make-head", `{"head":"`+strings.Repeat("b", 64)+`"}`, op, map[string]string{CSRFHeader: csrfOf(t, h, op)})
	if w.Code != 503 || !strings.Contains(w.Body.String(), "make_head_unavailable") {
		t.Errorf("make-head without blobs: %d %s", w.Code, w.Body)
	}
}

// A head change whose normalize kick fails still stands, and the
// operator is told the transcript waits for the next start.
func TestMakeHeadReportsAFailedNormalizeKick(t *testing.T) {
	lake, idp, _, dir := rawLake(t, nil)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"ops": "operator"}}}
	reg := &Registrations{
		Lake:      func() registrar.Lake { return registrar.Lake{Catalog: lake.Catalog, Dir: dir} },
		Blobs:     lake.CAS,
		Normalize: func(context.Context) (int, error) { return 0, errors.New("queue closed") },
	}
	web, err := New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, reg, nil, idp.Client())
	if err != nil {
		t.Fatal(err)
	}
	lake.Web = web
	h := lake.Handler()
	uid, id := forkSession(t, lake, "forked", "one\n", "two\n")
	op := signInAs(t, idp, h, "ops")
	head := putBlob(t, lake, []byte("one\n"))
	w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrfOf(t, h, op)}, "head": {head}}, op)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "could not be started") || !strings.Contains(w.Body.String(), "Made the head") {
		t.Errorf("failed kick: %d", w.Code)
	}
	if sum, err := lake.Catalog.DashboardSession(t.Context(), catalog.AllBays(), uid); err != nil || sum.HeadSHA256 != putBlob(t, lake, []byte("two\n")) {
		t.Errorf("the head change did not stand: %s %v", sum.HeadSHA256, err)
	}
	if jobs, err := lake.Catalog.ListNormalizeJobs(t.Context()); err != nil || len(jobs) != 1 {
		t.Errorf("the job row is not left for the next start: %+v %v", jobs, err)
	}
}

// A copy with the head's bytes at another path is still an open conflict
// with actions; only the artifact that is the head has nothing to
// settle.
func TestSameBytesAtAnotherPathStayActionable(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	_, id := forkSession(t, lake, "forked", "one\n", "two\n")
	two := putBlob(t, lake, []byte("two\n"))
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-c", Harness: "codex", NativeSessionID: "forked",
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "other/forked/rollout.jsonl", SHA256: two, Size: 4}}}
	ack, err := lake.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	twin := ack.ArtifactIDs[0]
	op := signInAs(t, idp, h, "ops")
	if w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrfOf(t, h, op)}, "head": {putBlob(t, lake, []byte("one\n"))}}, op); w.Code != 303 {
		t.Fatalf("make-head: %d", w.Code)
	}
	if page := get(h, conflictURL(id), op).Body.String(); !strings.Contains(page, "There is nothing to settle.") {
		t.Error("the new head offers actions")
	}
	page := get(h, conflictURL(twin), op).Body.String()
	if strings.Contains(page, "There is nothing to settle.") || !strings.Contains(page, "Keep the head") || !strings.Contains(page, "The copy and the head are the same bytes.") {
		t.Error("an open copy with the head's bytes has no actions")
	}
}

// When the audit line cannot be written, the new head is still
// normalized at once.
func TestMakeHeadKicksNormalizeWhenTheAuditFlushFails(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	_, id := forkSession(t, lake, "forked", "one\n", "two\n")
	op := signInAs(t, idp, h, "ops")
	csrf := csrfOf(t, h, op)
	if err := os.RemoveAll(audit.Path(dir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(audit.Path(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrf}, "head": {putBlob(t, lake, []byte("one\n"))}}, op)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "audit log failed") {
		t.Errorf("flush failure: %d", w.Code)
	}
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	if jobs, err := lake.Catalog.ListNormalizeJobs(t.Context()); err != nil || len(jobs) != 0 {
		t.Errorf("the job waited for the next start: %+v %v", jobs, err)
	}
}
