package web

import (
	"net/http"
	"strconv"

	"terva.sh/lampi/internal/catalog"
)

type pagerView struct {
	Current, Total              int
	First, Previous, Next, Last string
	Pages                       []pageLink
}

type pageLink struct {
	Number       int
	URL          string
	Current, Gap bool
}

func pager(r *http.Request, nav catalog.PageNavigation, generation *int64) pagerView {
	if len(nav.Cursors) == 0 {
		return pagerView{}
	}
	p := pagerView{Current: nav.Current + 1, Total: len(nav.Cursors)}
	link := func(i int) string {
		q := r.URL.Query()
		q.Del("cursor")
		q.Del("from")
		q.Del("at")
		if generation != nil {
			q.Set("gen", strconv.FormatInt(*generation, 10))
		}
		if nav.Cursors[i] != "" {
			q.Set("cursor", nav.Cursors[i])
		}
		if len(q) == 0 {
			return r.URL.Path
		}
		return r.URL.Path + "?" + q.Encode()
	}
	if nav.Current > 0 {
		p.First, p.Previous = link(0), link(nav.Current-1)
	}
	if nav.Current < p.Total-1 {
		p.Next, p.Last = link(nav.Current+1), link(p.Total-1)
	}
	previous := -1
	for i := 0; i < p.Total; i++ {
		if i != 0 && i != p.Total-1 && (i < nav.Current-2 || i > nav.Current+2) {
			continue
		}
		if previous >= 0 && i > previous+1 {
			p.Pages = append(p.Pages, pageLink{Gap: true})
		}
		p.Pages = append(p.Pages, pageLink{Number: i + 1, URL: link(i), Current: i == nav.Current})
		previous = i
	}
	return p
}
