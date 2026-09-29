package web

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
	"terva.sh/lampi/internal/webconfig"
)

func TestSearchAPIIsGuardedValidatedAndCurrent(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "searchable")
	cookie, _ := signIn(t, idp, h)
	publishEvents(t, lake, uid, 12, func(i int) string {
		if i == 7 {
			return `ran <script>alert(1)</script> git push --force now`
		}
		return fmt.Sprint("ordinary ", i)
	})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	path := "/api/web/v1/search?q=" + url.QueryEscape("GIT PUSH --force")
	if get(h, path, nil).Code != 401 {
		t.Fatal("unguarded search")
	}
	r := httptest.NewRequest("GET", "https://lake.example"+path, nil)
	r.Header.Set("Authorization", "Bearer "+strings.Repeat("a", 64))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("device token authorizes search", w.Code)
	}
	w = get(h, path, cookie)
	var page recall.SearchPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatal("search", w.Code, w.Body.String())
	}
	hit := page.Items[0]
	if hit.Position != 7 || hit.NativeID != "searchable" || hit.SessionUID != uid || !strings.Contains(hit.Link, "at=7") || page.Coverage.Indexed != 0 {
		// Coverage counts the whole lake; a viewer is not told it.
		t.Fatalf("hit %+v coverage %+v", hit, page.Coverage)
	}
	for _, bad := range []string{"", "?q=", "?q=ab", "?q=abc&since=yesterday", "?q=abc&since=2026-09-03&until=2026-09-02", "?q=abc&x=1", "?q=abc&limit=201", "?q=abc&q=def", "?q=abc&unlinked=maybe", "?q=abc&cursor=nope", "?q=abc&harness=vim"} {
		if w := get(h, "/api/web/v1/search"+bad, cookie); w.Code != 400 {
			t.Fatal("accepted", bad, w.Code)
		}
	}
	// Date-only until covers the whole day; the fixture events are
	// recorded 2026-09-26.
	for q, want := range map[string]int{"&until=2026-09-26": 1, "&until=2026-09-25": 0, "&since=2026-09-26": 1, "&since=2026-09-27T00:00:00Z": 0} {
		w := get(h, "/api/web/v1/search?q=push"+q, cookie)
		var p recall.SearchPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Items) != want {
			t.Fatal("date filter", q, w.Code, len(p.Items))
		}
	}
	// A newer generation hides the old hit before the index catches up.
	publishEvents(t, lake, uid, 1, func(int) string { return "replaced" })
	w = get(h, path, cookie)
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 0 {
		t.Fatal("stale hit served", w.Body.String())
	}
}

func TestSearchPageMarksMatchesAndEscapes(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, `<b>native</b>`)
	cookie, _ := signIn(t, idp, h)
	publishEvents(t, lake, uid, 3, func(i int) string {
		return fmt.Sprintf(`line %d: <img src=x onerror=alert(1)> needle "quoted" end`, i)
	})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	w := get(h, "/search", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `role="search"`) || strings.Contains(w.Body.String(), `class="hits"`) || strings.Contains(w.Body.String(), "Searching ") {
		t.Fatal("empty form", w.Code)
	}
	w = get(h, "/search?q="+url.QueryEscape(`needle "quoted"`)+"&harness=&project=&since=&until=", cookie)
	body := w.Body.String()
	if w.Code != 200 || strings.Count(body, `<li class="hit">`) != 3 || !strings.Contains(body, "<mark>needle &#34;quoted&#34;</mark>") {
		t.Fatal("results", w.Code, body)
	}
	if strings.Contains(body, "<img") || strings.Contains(body, "<b>native") || !strings.Contains(body, "&lt;b&gt;native&lt;/b&gt;") {
		t.Fatal("unescaped content")
	}
	if !strings.Contains(body, `href="/sessions/`+uid+`/transcript?at=2&amp;gen=`) {
		t.Fatal("deep link missing")
	}
	if w := get(h, "/search?q=ab", cookie); w.Code != 400 || !strings.Contains(w.Body.String(), "That search cannot run") {
		t.Fatal("short query", w.Code)
	}
	if w := get(h, "/search?q=&harness=codex", cookie); w.Code != 400 {
		t.Fatal("filters without text", w.Code)
	}
	if w := get(h, "/search?q=absent-text", cookie); !strings.Contains(w.Body.String(), "No matches") {
		t.Fatal("no matches")
	}
	w = get(h, "/search?q=needle&limit=1", cookie)
	if !strings.Contains(w.Body.String(), "Next page") {
		t.Fatal("paging link")
	}
}

func TestSearchOffWithoutIndex(t *testing.T) {
	lake, idp, _, _ := fixture(t)
	cfg := webconfig.Config{BaseURL: "https://lake.example", OIDC: webconfig.OIDC{Issuer: idp.URL(), ClientID: "lake", RoleMap: map[string]string{"readers": "viewer"}}}
	web, err := New(cfg, lake.Catalog, recall.NewReader(lake.Catalog, lake.Normalized), nil, nil, nil, idp.Client(), slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	lake.Web = web
	h := lake.Handler()
	cookie, _ := signIn(t, idp, h)
	if w := get(h, "/api/web/v1/search?q=abc", cookie); w.Code != 503 || !strings.Contains(w.Body.String(), "search_unavailable") {
		t.Fatal("api", w.Code)
	}
	if w := get(h, "/search", cookie); w.Code != 503 || !strings.Contains(w.Body.String(), "Search is not enabled") {
		t.Fatal("page", w.Code)
	}
}

func TestStructuredSearchThroughAPIAndPage(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "structured")
	cookie, _ := signIn(t, idp, h)
	gen, err := lake.Catalog.EnqueueNormalize(t.Context(), uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bash, yes, no := "Bash", true, false
	evs := make([]normalize.Event, 6)
	for i := range evs {
		txt := fmt.Sprint("output ", i)
		evs[i] = normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint(i), SessionID: "s", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z", Actor: normalize.ActorTool, EventType: normalize.EventToolResult, ContentText: &txt, Tool: normalize.Tool{Name: &bash}, RawType: "function_call_output", Redaction: normalize.Redaction{Status: "none"}}
		switch i {
		case 4:
			evs[i].Tool.IsError = &yes
		case 5:
			// Claude Code records is_error false on a result that succeeded.
			evs[i].Tool.IsError = &no
		}
	}
	if err := lake.StoreEvents(t.Context(), uid, evs, nil); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.DeleteNormalizeJob(t.Context(), uid, gen); err != nil {
		t.Fatal(err)
	}
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	for q, want := range map[string]int{
		"event_type=tool_result": 6, "tool_error=true": 1, "tool=Bash&tool_error=true&actor=tool": 1,
		"raw_type=function_call_output": 6, "q=output+4&tool=Bash": 1, "tool=bash": 0, "tool_error=false": 1,
	} {
		w := get(h, "/api/web/v1/search?"+q, cookie)
		var p recall.SearchPage
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || len(p.Items) != want {
			t.Fatal(q, w.Code, len(p.Items), w.Body.String())
		}
	}
	for _, q := range []string{"event_type=nonsense", "actor=robot", "tool_error=maybe", "harness=codex", "tool_error="} {
		if w := get(h, "/api/web/v1/search?"+q, cookie); w.Code != 400 {
			t.Fatal("accepted", q, w.Code)
		}
	}
	w := get(h, "/search?q=&event_type=&actor=&tool=Bash&raw_type=&tool_error=true&harness=&project=&since=&until=", cookie)
	if w.Code != 200 || strings.Count(w.Body.String(), `<li class="hit">`) != 1 || !strings.Contains(w.Body.String(), "tool error") {
		t.Fatal("structured page", w.Code)
	}
	// Only the result that failed is marked, not one recorded as not failing.
	w = get(h, "/search?q=&event_type=tool_result&actor=&tool=Bash&raw_type=&harness=&project=&since=&until=", cookie)
	if w.Code != 200 || strings.Count(w.Body.String(), `<li class="hit">`) != 6 || strings.Count(w.Body.String(), "tool error</span>") != 1 {
		t.Fatal("tool error badges", w.Code, strings.Count(w.Body.String(), "tool error</span>"))
	}
}

// A read that fails with an error the page does not recognise is a 500
// named only read_failed, and the lake's access log line for it carries
// the error, so the cause can be found (TKT-01M3MHBT5).
func TestReadFailureIsLogged(t *testing.T) {
	lake, idp, h, logs := fixture(t)
	cookie, _ := signIn(t, idp, h)
	if err := indexes[lake].Close(); err != nil {
		t.Fatal(err)
	}
	w := get(h, "/search?q=anything", cookie)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "read_failed") || strings.Contains(w.Body.String(), "closed") {
		t.Fatal("page", w.Code, w.Body.String())
	}
	var line string
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, "path=/search") && strings.Contains(l, "status=500") {
			line = l
		}
	}
	if !strings.Contains(line, "err=") || !strings.Contains(line, "closed") {
		t.Fatalf("access log line %q", line)
	}
}
