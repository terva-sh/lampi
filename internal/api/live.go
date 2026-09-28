package api

import (
	"context"
	"fmt"
	"sort"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
)

// liveDigests is every CAS entry a lake needs: each digest the catalog
// and the sessions' last manifests name, and everything those read
// from, following chunk lists and prefix records down. serve compact
// keeps it in the lake and serve backup --prune keeps it in a backup,
// so the two agree on what a lake is.
//
// planned, when set, gives the base a prefix record not yet written
// would name; compact's dry run passes its planned folds. missing is
// the named digests the store has neither an object nor a logical
// entry for, sorted.
func liveDigests(ctx context.Context, cat *catalog.Catalog, store *cas.Store, sessions []catalog.SessionInfo, planned func(string) (string, bool)) (keep map[string]bool, missing []string, err error) {
	named, err := cat.ReferencedDigests(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, info := range sessions {
		for _, a := range info.Manifest.Artifacts {
			addNamed(named, a.SHA256, a.TailSHA256, a.ChunkSHA256s...)
		}
	}
	keep = make(map[string]bool, len(named))
	queue := make([]string, 0, len(named))
	for d := range named {
		queue = append(queue, d)
	}
	for len(queue) > 0 {
		d := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if keep[d] {
			continue
		}
		keep[d] = true
		if planned != nil {
			if base, ok := planned(d); ok {
				queue = append(queue, base)
				continue
			}
		}
		object, isLogical, links, err := store.Stored(d)
		if err != nil {
			return nil, nil, err
		}
		if !object && !isLogical {
			missing = append(missing, d)
			continue
		}
		// A logical entry that does not parse hides what it reads
		// from. Stop rather than remove what it needs.
		if isLogical && len(links) == 0 {
			return nil, nil, fmt.Errorf("logical index %s is unreadable; run serve fsck", d)
		}
		queue = append(queue, links...)
	}
	sort.Strings(missing)
	return keep, missing, nil
}
