package web

import (
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"terva.sh/lampi/internal/catalog"
)

func pagerLink(t *testing.T, body, label string) string {
	t.Helper()
	re := regexp.MustCompile(`href="([^"]+)" aria-label="` + regexp.QuoteMeta(label) + `"`)
	links := re.FindAllStringSubmatch(body, -1)
	if len(links) != 2 || links[0][1] != links[1][1] {
		t.Fatalf("expected matching top/bottom %s links, got %v", label, links)
	}
	return html.UnescapeString(links[0][1])
}

func TestPagerWindowAndPreservedFilters(t *testing.T) {
	r := httptest.NewRequest("GET", "/sessions/s/transcript?limit=7&gen=2&at=44&from=39", nil)
	nav := catalog.PageNavigation{Cursors: make([]string, 20), Current: 10}
	for i := range nav.Cursors {
		nav.Cursors[i] = fmt.Sprint("cursor-", i)
	}
	gen := int64(2)
	p := pager(r, nav, &gen)
	if p.Current != 11 || p.Total != 20 || len(p.Pages) != 9 {
		t.Fatalf("pager %+v", p)
	}
	for _, path := range []string{p.First, p.Previous, p.Next, p.Last} {
		u, _ := url.Parse(path)
		q := u.Query()
		if q.Get("limit") != "7" || q.Get("gen") != "2" || q.Has("from") || q.Has("at") {
			t.Fatal("lost size or pin, or retained target", path)
		}
	}
	q := httptest.NewRequest("GET", "/search?q=needle&actor=user&limit=3&since=2026-09-26&until=2026-09-26", nil)
	p = pager(q, catalog.PageNavigation{Cursors: []string{"", "next"}}, nil)
	if p.First != "" || p.Previous != "" || !strings.Contains(p.Last, "actor=user") || !strings.Contains(p.Last, "q=needle") {
		t.Fatal("first-page filters or disabled links", p)
	}
	p = pager(q, catalog.PageNavigation{Cursors: []string{"", "next"}, Current: 1}, nil)
	if p.Next != "" || p.Last != "" || p.First == "" || p.Previous == "" {
		t.Fatal("last-page links", p)
	}
	for _, path := range []string{p.First, p.Previous} {
		u, err := url.Parse(path)
		if err != nil || u.Query().Get("since") != "2026-09-26" || u.Query().Get("until") != "2026-09-26" {
			t.Fatal("lost search date bounds", path, err)
		}
	}
}

func TestDashboardListsHaveTwoWorkingPagers(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	for i := 0; i < 5; i++ {
		seedSession(t, lake.Catalog, fmt.Sprintf("paging-%d", i))
	}
	uid := seedSession(t, lake.Catalog, "transcript-pages")
	cookie, _ := signIn(t, idp, h)
	publishEvents(t, lake, uid, 23, func(i int) string { return fmt.Sprintf("needle %d", i) })
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/sessions?limit=2", "/search?q=needle&limit=7", "/search?q=needle&limit=7&since=2026-09-26&until=2026-09-26", "/sessions/" + uid + "/transcript?limit=7"} {
		w := get(h, path, cookie)
		if w.Code != 200 || strings.Count(w.Body.String(), `aria-label="Pagination"`) != 2 {
			t.Fatal("missing pagers", path, w.Code)
		}
		first := w.Body.String()
		if strings.Contains(path, "since=") {
			for _, label := range []string{"Next page", "Last page"} {
				u, err := url.Parse(pagerLink(t, first, label))
				if err != nil || u.Query().Get("since") != "2026-09-26" || u.Query().Get("until") != "2026-09-26" {
					t.Fatal("lost search date bounds", label, u, err)
				}
			}
		}
		last := get(h, pagerLink(t, first, "Last page"), cookie)
		if last.Code != 200 || strings.Contains(last.Body.String(), `aria-label="Next page"`) || !strings.Contains(last.Body.String(), `aria-current="page" aria-label="Page `) {
			t.Fatal("last page", path, last.Code)
		}
		back := get(h, pagerLink(t, last.Body.String(), "First page"), cookie)
		if back.Code != 200 || !strings.Contains(back.Body.String(), "Page 1 of") {
			t.Fatal("first page", path, back.Code)
		}
		prev := get(h, pagerLink(t, last.Body.String(), "Previous page"), cookie)
		if prev.Code != 200 || strings.Contains(prev.Body.String(), `aria-disabled="true">Next`) {
			t.Fatal("previous page", path, prev.Code)
		}
	}
	for _, path := range []string{"/sessions/" + uid + "?collection=artifacts", "/sessions/" + uid + "?collection=provenance", "/sessions/" + uid + "?collection=conflicts", "/conflicts"} {
		w := get(h, path, cookie)
		if w.Code != 200 || strings.Count(w.Body.String(), `aria-label="Pagination"`) != 2 || !strings.Contains(w.Body.String(), "Page 1 of 1") {
			t.Fatal("single/empty metadata page", path, w.Code, w.Body.String())
		}
	}
}
