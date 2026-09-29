package web

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// routeLiteral finds every path the web package names in its source: a
// route pattern, alone or after "GET ". New routes are found without
// anyone listing them here.
var routeLiteral = regexp.MustCompile(`"(?:GET )?(/[A-Za-z0-9/{}$._-]*)"`)

// TestNoRouteShowsASessionOutsideTheViewersBays signs in as a viewer
// who reads only the default bay and asks every GET route for a session
// in another bay, by uid and by what is in it. No answer may name it
// (TKT-01M3NNF27A). A route added later is asked too.
func TestNoRouteShowsASessionOutsideTheViewersBays(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	ctx := t.Context()
	secret, secretDigest := storeSession(t, lake, "native-zq-secret", []byte("{}\n"))
	open, _ := storeSession(t, lake, "native-open", []byte("{}\n{}\n"))
	if _, err := lake.Catalog.CreateBay(ctx, "secret", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: secret, Bay: "secret", Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: secret, Bay: catalog.DefaultBayName, Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	publishEvents(t, lake, secret, 3, func(int) string { return "zqsecretword in the secret bay" })
	publishEvents(t, lake, open, 3, func(int) string { return "zqsecretword in the inbox" })
	if err := indexes[lake].Pass(ctx); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)

	// The viewer does read the inbox, so an empty answer everywhere
	// would not pass by accident.
	if w := get(h, "/api/web/v1/sessions", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), open) {
		t.Fatalf("viewer cannot see the inbox: %d %s", w.Code, w.Body)
	}
	if w := get(h, "/api/web/v1/search?q=zqsecretword", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), open) {
		t.Fatalf("viewer cannot search the inbox: %d %s", w.Code, w.Body)
	}

	paths := map[string]bool{}
	files, _ := filepath.Glob("*.go")
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeLiteral.FindAllStringSubmatch(string(src), -1) {
			paths[m[1]] = true
		}
	}
	fill := strings.NewReplacer("{uid}", secret, "{digest}", secretDigest, "{collection}", "artifacts", "{id}", "none", "{name}", "default", "{$}", "")
	var asked []string
	for p := range paths {
		if strings.HasPrefix(p, "/assets") || strings.HasPrefix(p, "/auth") {
			continue
		}
		base := fill.Replace(p)
		asked = append(asked, base, base+"?q=zqsecretword", base+"?at=0", strings.Replace(base, "artifacts", "provenance", 1))
	}
	sort.Strings(asked)
	answered := 0
	for _, p := range asked {
		w := get(h, p, cookie)
		if w.Code == 200 {
			answered++
		}
		body := w.Body.String()
		for _, leak := range []string{secret, "native-zq-secret", "in the secret bay"} {
			if strings.Contains(body, leak) {
				t.Errorf("GET %s (%d) shows %q from a bay the viewer does not read", p, w.Code, leak)
			}
		}
	}
	if answered < 10 {
		t.Fatalf("only %d of %d routes answered 200; the test is not reaching the pages", answered, len(asked))
	}
}

// An operator who does not read a conflict's bay can neither see it nor
// keep its head, make it the head or reopen it: each answers as if it
// were not there.
func TestOperatorActsOnlyOnConflictsInTheirBays(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	uid, id := forkSession(t, lake, "forked", "one\ntwo\n", "one\nzzz\n")
	ctx := t.Context()
	if _, err := lake.Catalog.CreateBay(ctx, "secret", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: "secret", Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: uid, Bay: catalog.DefaultBayName, Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	op := signInAs(t, idp, h, "ops")
	csrf := csrfOf(t, h, op)
	if w := get(h, conflictURL(id), op); w.Code != 404 {
		t.Errorf("page: %d", w.Code)
	}
	for _, action := range []string{"keep-head", "reopen"} {
		if w := post(h, "/api/web/v1/conflicts/"+id+"/"+action, "", op, map[string]string{CSRFHeader: csrf}); w.Code != 404 {
			t.Errorf("api %s: %d %s", action, w.Code, w.Body)
		}
		if w := postForm(h, conflictURL(id)+"/"+action, url.Values{"csrf": {csrf}}, op); w.Code != 404 {
			t.Errorf("form %s: %d", action, w.Code)
		}
	}
	// make-head with the real head, so only the scope stops it.
	head := putBlob(t, lake, []byte("one\ntwo\n"))
	if w := post(h, "/api/web/v1/conflicts/"+id+"/make-head", `{"head":"`+head+`"}`, op, map[string]string{CSRFHeader: csrf}); w.Code != 404 {
		t.Errorf("api make-head: %d %s", w.Code, w.Body)
	}
	if w := postForm(h, conflictURL(id)+"/make-head", url.Values{"csrf": {csrf}, "head": {head}}, op); w.Code != 404 {
		t.Errorf("form make-head: %d", w.Code)
	}
	if d, ok, err := lake.Catalog.Conflict(ctx, catalog.AllBays(), id); err != nil || !ok || d.Resolution != nil || d.HeadSHA256 != head {
		t.Errorf("the conflict changed: %+v %v %v", d, ok, err)
	}
}
