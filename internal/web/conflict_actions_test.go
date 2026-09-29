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

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
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
	if w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrf}, "note": {"bad\nnote"}}, op); w.Code != 400 || !strings.Contains(w.Body.String(), "one line") {
		t.Errorf("multi-line note: %d", w.Code)
	}
	w := postForm(h, conflictURL(id)+"/keep-head", url.Values{"csrf": {csrf}, "note": {"same session, other laptop"}}, op)
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
	if w := post(h, "/api/web/v1/conflicts/"+id+"/keep-head", `{"note":"x","extra":1}`, op, hdr); w.Code != 400 {
		t.Errorf("unknown field: %d", w.Code)
	}
	if w := post(h, "/api/web/v1/conflicts/"+id+"/reopen", `{"note":"x"}`, op, hdr); w.Code != 400 {
		t.Errorf("reopen with a note: %d", w.Code)
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
