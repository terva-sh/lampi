package web

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// seedConflict stores a divergent copy of the session native and
// returns the session and the copy's artifact id.
func seedConflict(t *testing.T, c *catalog.Catalog, native string) (string, string) {
	t.Helper()
	uid := seedSession(t, c, native)
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-b", Harness: "codex", NativeSessionID: native, Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "session.jsonl", SHA256: strings.Repeat("c", 64), Size: 9}}}
	ack, err := c.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationDivergentCopy, Record: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return uid, ack.ArtifactIDs[0]
}

// A resolved conflict leaves the conflict lists and the overview count,
// and resolved=true brings it back with its resolution (TKT-01M3PTMKG9).
func TestResolvedConflictsLeaveTheLists(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid, id := seedConflict(t, lake.Catalog, "forked")
	cookie, _ := signIn(t, idp, h)
	if w := get(h, "/api/web/v1/overview", cookie); !strings.Contains(w.Body.String(), `"conflicts":1`) {
		t.Fatalf("overview before: %s", w.Body)
	}
	if err := lake.Catalog.ResolveConflict(t.Context(), id, catalog.ResolutionKeptHead, "user:ada", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := get(h, "/api/web/v1/overview", cookie); !strings.Contains(w.Body.String(), `"conflicts":0`) {
		t.Fatalf("overview after: %s", w.Body)
	}
	for _, path := range []string{"/api/web/v1/conflicts", "/api/web/v1/sessions/" + uid + "/conflicts", "/api/web/v1/conflicts?resolved=false"} {
		w := get(h, path, cookie)
		if w.Code != 200 || strings.Contains(w.Body.String(), id) {
			t.Errorf("%s lists a resolved conflict: %d %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/api/web/v1/conflicts?resolved=true", "/api/web/v1/sessions/" + uid + "/conflicts?resolved=true"} {
		w := get(h, path, cookie)
		if w.Code != 200 || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), `"resolution":"kept_head"`) {
			t.Errorf("%s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, path := range []string{"/conflicts?resolved=true", "/sessions/" + uid + "?collection=conflicts&resolved=true"} {
		if w := get(h, path, cookie); w.Code != 200 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/web/v1/conflicts?resolved=maybe", "/api/web/v1/conflicts?current=true", "/api/web/v1/sessions/" + uid + "/artifacts?resolved=true", "/api/web/v1/sessions?resolved=true", "/sessions/" + uid + "?resolved=true"} {
		if w := get(h, path, cookie); w.Code != 400 {
			t.Errorf("%s accepted: %d", path, w.Code)
		}
	}
}

// The Conflicts page explains itself, names the machine behind each
// side, flags a shorter copy, and says when there is nothing to do
// (TKT-01M3PTMWF6).
func TestConflictsPageExplainsEachRow(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	cookie, _ := signIn(t, idp, h)
	body := get(h, "/conflicts", cookie).Body.String()
	for _, want := range []string{"What a conflict is, and what to check", "No open conflicts", "Nothing to do.", `href="/conflicts?resolved=true"`} {
		if !strings.Contains(body, want) {
			t.Errorf("empty page lacks %q", want)
		}
	}

	uid, id := seedConflict(t, lake.Catalog, "forked")
	devs, err := lake.Catalog.SyncTokenFile(t.Context(), []catalog.TokenEntry{{Hash: strings.Repeat("1", 64), Name: "laptop"}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.BindMachine(t.Context(), devs[0].ID, "machine-a", time.Now()); err != nil {
		t.Fatal(err)
	}
	body = get(h, "/conflicts", cookie).Body.String()
	for _, want := range []string{`title="machine-a">laptop<`, `title="machine-b">machine-b<`, ">shorter<", ">Open<", "12 B", "9 B"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if err := lake.Catalog.ResolveConflict(t.Context(), id, catalog.ResolutionNotAConflict, "op", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if body = get(h, "/conflicts", cookie).Body.String(); strings.Contains(body, strings.Repeat("c", 64)) || !strings.Contains(body, "No open conflicts") {
		t.Error("a resolved conflict is still on the open list")
	}
	tab := get(h, "/sessions/"+uid+"?collection=conflicts&resolved=true", cookie).Body.String()
	if !strings.Contains(tab, "Not a conflict") || !strings.Contains(tab, strings.Repeat("c", 64)) || strings.Contains(tab, "What a conflict is") {
		t.Error("session tab with resolved conflicts")
	}
}
