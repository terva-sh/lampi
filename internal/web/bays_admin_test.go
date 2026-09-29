package web

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

// TKT-01M3NNF2K3: the bays page and its changes are the admin's.
func TestBaysPageIsAdminOnly(t *testing.T) {
	_, idp, h, _ := rawLake(t, nil)
	for _, group := range []string{"readers", "ops"} {
		c := signInAs(t, idp, h, group)
		if w := get(h, adminBaysPath, c); w.Code != http.StatusNotFound {
			t.Errorf("%s page: %d", group, w.Code)
		}
		if w := postForm(h, adminBaysPath+"/move", url.Values{"csrf": {csrfOf(t, h, c)}}, c); w.Code != http.StatusNotFound {
			t.Errorf("%s move: %d", group, w.Code)
		}
		if strings.Contains(get(h, "/", c).Body.String(), `href="`+adminBaysPath+`"`) {
			t.Errorf("%s sees the link", group)
		}
	}
	admin := signInAs(t, idp, h, "owners")
	if !strings.Contains(get(h, "/", admin).Body.String(), `href="`+adminBaysPath+`"`) {
		t.Fatal("admin has no link")
	}
}

// A session page names only the bays its reader reads.
func TestSessionPageNamesOnlyTheReadersBays(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	uid := ingestHead(t, lake, "sess-1", strings.Repeat("ab", 32), 4)
	if _, err := lake.Catalog.CreateBay(ctx, "client-secret", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: "client-secret", Actor: "admin", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	viewer := signInAs(t, idp, h, "readers")
	body := get(h, "/sessions/"+uid, viewer).Body.String()
	if !strings.Contains(body, "<dt>Bays</dt><dd>default</dd>") || strings.Contains(body, "client-secret") {
		t.Fatalf("viewer's session page names %q", between(body, "<dt>Bays</dt>", "</dd>"))
	}
	admin := signInAs(t, idp, h, "owners")
	if body := get(h, "/sessions/"+uid, admin).Body.String(); !strings.Contains(body, "<dt>Bays</dt><dd>client-secret, default</dd>") {
		t.Fatalf("admin's session page names %q", between(body, "<dt>Bays</dt>", "</dd>"))
	}
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	if i < 0 {
		return ""
	}
	s = s[i+len(from):]
	if j := strings.Index(s, to); j >= 0 {
		return s[:j]
	}
	return s
}

func TestAdminMovesASessionAndReleasesAHold(t *testing.T) {
	lake, idp, h, dir := rawLake(t, nil)
	ctx := t.Context()
	plain := ingestHead(t, lake, "sess-plain", strings.Repeat("ab", 32), 4)
	work, err := lake.Catalog.CreateBay(ctx, "work", "admin", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.CreateBay(ctx, "review", "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddBayRule(ctx, catalog.BayRule{Match: config.ProjectMatch{CWDPrefix: "/held"}, Action: catalog.RuleHold, BayID: "review"}, "admin", time.Now()); err != nil {
		t.Fatal(err)
	}
	held := ingestHeld(t, lake, "sess-held")

	admin := signInAs(t, idp, h, "owners")
	page := get(h, adminBaysPath, admin).Body.String()
	for _, want := range []string{plain, held, catalog.ReasonNothingPlaced, "held by hold rule 1 into bay review", `action="/admin/bays/release"`} {
		if !strings.Contains(page, want) {
			t.Errorf("bays page lacks %q", want)
		}
	}
	csrf := csrfOf(t, h, admin)
	if w := postForm(h, adminBaysPath+"/move", url.Values{"csrf": {"wrong"}, "uid": {plain}, "from": {catalog.DefaultBayID}, "to": {work.ID}}, admin); w.Code != http.StatusForbidden {
		t.Fatalf("bad csrf: %d", w.Code)
	}
	if w := postForm(h, adminBaysPath+"/move", url.Values{"csrf": {csrf}, "uid": {plain}, "from": {catalog.DefaultBayID}, "to": {catalog.DefaultBayID}}, admin); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), bayProblems["same_bay"]) {
		t.Fatalf("same bay: %d", w.Code)
	}
	w := postForm(h, adminBaysPath+"/move", url.Values{"csrf": {csrf}, "uid": {plain}, "from": {catalog.DefaultBayID}, "to": {work.ID}}, admin)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Moved "+plain) {
		t.Fatalf("move %d: %s", w.Code, between(w.Body.String(), "<main", "</section>"))
	}
	if got, _ := lake.Catalog.SessionBays(ctx, plain); len(got) != 1 || got[0] != work.ID {
		t.Fatalf("after the move: %v", got)
	}
	// A held session is placed by a release, not a move (review 1468).
	if w := postForm(h, adminBaysPath+"/move", url.Values{"csrf": {csrf}, "uid": {held}, "from": {"review"}, "to": {work.ID}}, admin); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "held for review") {
		t.Fatalf("move of a held session: %d", w.Code)
	}
	if w := postForm(h, adminBaysPath+"/release", url.Values{"csrf": {csrf}, "uid": {held}}, admin); w.Code != http.StatusOK {
		t.Fatalf("release %d", w.Code)
	}
	if got, _ := lake.Catalog.SessionBays(ctx, held); len(got) != 1 || got[0] != catalog.DefaultBayID {
		t.Fatalf("after the release: %v", got)
	}
	if w := postForm(h, adminBaysPath+"/release", url.Values{"csrf": {csrf}, "uid": {held}}, admin); w.Code != http.StatusBadRequest {
		t.Fatalf("second release: %d", w.Code)
	}
	raw, err := os.ReadFile(audit.Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"bay.member.added"`, `"kind":"bay.hold.released"`, "via web", `"actor":"web:`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("audit lacks %s", want)
		}
	}
}

// Moving and releasing add a session to a bay, so they need a recent
// sign-in.
func TestBayChangesNeedAFreshSignIn(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid := ingestHead(t, lake, "sess-1", strings.Repeat("ab", 32), 4)
	idp.AuthTime = time.Now().Add(-time.Hour)
	admin := signInAs(t, idp, h, "owners")
	if strings.Contains(get(h, adminBaysPath, admin).Body.String(), `action="/admin/bays/move"`) {
		t.Fatal("a stale sign-in was offered the move form")
	}
	for _, action := range []string{"/move", "/release"} {
		w := postForm(h, adminBaysPath+action, url.Values{"csrf": {csrfOf(t, h, admin)}, "uid": {uid}, "from": {"default"}, "to": {"default"}}, admin)
		if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "fresh=1") {
			t.Errorf("%s with a stale sign-in: %d %v", action, w.Code, w.Header())
		}
	}
}
