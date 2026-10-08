package recall

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"terva.sh/lampi/internal/catalog"
)

type eventNavigationKey struct {
	UID              string
	Gen, Size, MTime int64
	Limit            int
}

// SearchNumbered builds boundaries from lightweight matching keys, excluding
// stale generations and sessions outside scope before counting any pages.
// It reads content only for the selected page; API keyset paging is unchanged.
func (x *Index) SearchNumbered(ctx context.Context, req SearchRequest) (SearchPage, catalog.PageNavigation, error) {
	var page SearchPage
	before, err := req.validate(x.reader)
	if err != nil {
		return page, catalog.PageNavigation{}, err
	}
	query, args := searchSQL(req, -1)
	query = "SELECT d.id,d.session_uid,i.gen" + query[strings.Index(query, "\n\t\tFROM "):]
	query = strings.TrimSuffix(query, " LIMIT ?")
	args = args[:len(args)-1]
	rows, err := x.db.QueryContext(ctx, query, args...)
	if err != nil {
		return page, catalog.PageNavigation{}, err
	}
	defer rows.Close()
	current := map[string]int64{}
	nav := catalog.PageNavigation{Cursors: []string{""}}
	ids := make([]int64, 0, req.Limit)
	count, skipped := 0, 0
	previous := int64(0)
	for rows.Next() {
		var id, gen int64
		var uid string
		if err := rows.Scan(&id, &uid, &gen); err != nil {
			return page, nav, err
		}
		live, seen := current[uid]
		if !seen {
			pub, err := x.reader.publication(ctx, uid)
			if err != nil && !errors.Is(err, ErrNotFound) {
				return page, nav, err
			}
			live = -1
			if err == nil && pub.State == "ready" {
				ok, err := x.reader.catalog.SessionInScope(ctx, req.Scope, uid)
				if err != nil {
					return page, nav, err
				}
				if ok {
					live = pub.Gen
				}
			}
			current[uid] = live
		}
		if live != gen {
			continue
		}
		if count > 0 && count%req.Limit == 0 {
			nav.Cursors = append(nav.Cursors, x.reader.signBody(searchCursor{V: 1, Filter: req.fingerprint(), Before: previous}))
		}
		if before > 0 && id >= before {
			skipped++
		} else if len(ids) < req.Limit+1 {
			ids = append(ids, id)
		}
		previous = id
		count++
	}
	if err := rows.Err(); err != nil {
		return page, nav, err
	}
	rows.Close()
	nav.Current = min(skipped/req.Limit, len(nav.Cursors)-1)
	req.ids = ids
	page, err = x.Search(ctx, req)
	return page, nav, err
}

// EventNavigation finds actual page boundaries, including the byte limit.
// Cached boundaries hold no content and are tied to the opened snapshot.
func (r *Reader) EventNavigation(ctx context.Context, scope catalog.Scope, page EventPage, limit int) (catalog.PageNavigation, error) {
	if err := r.inScope(ctx, scope, page.SessionUID); err != nil {
		return catalog.PageNavigation{}, err
	}
	if limit == 0 {
		limit = DefaultLimit
	}
	if limit < 1 || limit > MaxLimit {
		return catalog.PageNavigation{}, ErrInvalid
	}
	snap, err := r.open(ctx, page.SessionUID)
	if err != nil {
		return catalog.PageNavigation{}, err
	}
	defer snap.Close()
	if snap.pub.Gen != page.Generation || snap.pub.Head != page.Head {
		return catalog.PageNavigation{}, ErrGenerationChanged
	}
	key := eventNavigationKey{page.SessionUID, snap.pub.Gen, snap.size, snap.mtime, limit}
	r.navMu.Lock()
	starts := r.navCache[key]
	r.navMu.Unlock()
	if starts == nil {
		br, _, _, err := snap.lines(ctx, 0, 0)
		if err != nil {
			return catalog.PageNavigation{}, err
		}
		base := cursor{V: 1, UID: page.SessionUID, Gen: snap.pub.Gen, Head: snap.pub.Head, Size: snap.size, MTime: snap.mtime}
		starts = []cursor{base}
		pos, off := int64(0), int64(0)
		count, used := 0, 0
		for {
			if err := ctx.Err(); err != nil {
				return catalog.PageNavigation{}, err
			}
			line, n, err := readLine(br, MaxLine)
			if err == io.EOF && n == 0 {
				break
			}
			if err != nil && err != io.EOF {
				return catalog.PageNavigation{}, err
			}
			item := decodeItem(page.SessionUID, snap.pub.Gen, pos, line)
			b, _ := json.Marshal(item)
			if count == limit || count > 0 && used+len(b) > PageBytes {
				start := base
				start.Pos, start.Off = pos, off
				starts = append(starts, start)
				count, used = 0, 0
			}
			count++
			used += len(b)
			pos++
			if !snap.ev.Compressed {
				off += n
			}
			if err == io.EOF {
				break
			}
		}
		r.navMu.Lock()
		// Bound retained transcripts, and discard obsolete snapshots.
		if len(r.navCache) >= 8 {
			clear(r.navCache)
		}
		if r.navCache == nil {
			r.navCache = make(map[eventNavigationKey][]cursor)
		}
		r.navCache[key] = starts
		r.navMu.Unlock()
	}
	nav := catalog.PageNavigation{Cursors: make([]string, len(starts))}
	for i, start := range starts {
		nav.Cursors[i] = r.sign(start)
		if start.Pos <= page.From {
			nav.Current = i
		}
	}
	return nav, nil
}
