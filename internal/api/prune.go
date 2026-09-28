package api

import (
	"context"
	"fmt"
	"path/filepath"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
)

// PruneReport is what PruneBackup removed from a backup directory.
type PruneReport struct {
	Objects int
	Logical int
	// Bytes is the size of the objects removed. Logical entries are
	// small and are not counted.
	Bytes int64
	// Missing is the digests the backup's catalog names that its CAS
	// has no entry for. Pruning cannot make them worse, so they are
	// reported and do not stop it.
	Missing []string
}

// PruneBackup removes from the backup in dir every CAS entry its own
// catalog no longer reaches, by the same rule serve compact keeps a
// lake by. serve backup only adds to a backup, so without this a
// session serve purge removed, and the versions serve compact folded
// away, stay in every backup taken since.
//
// Call it only after a backup into dir finished: the catalog there is
// then the snapshot that the CAS copy completed. It opens that catalog
// read-only and changes nothing but dir/cas.
func PruneBackup(ctx context.Context, dir string) (PruneReport, error) {
	var rep PruneReport
	cat, err := catalog.OpenReadOnly(filepath.Join(dir, "catalog.db"))
	if err != nil {
		return rep, err
	}
	defer cat.Close()
	sessions, err := cat.ListSessions(ctx)
	if err != nil {
		return rep, err
	}
	store := &cas.Store{Root: filepath.Join(dir, "cas")}
	keep, missing, err := liveDigests(ctx, cat, store, sessions, nil)
	if err != nil {
		return rep, fmt.Errorf("prune %s: %w", dir, err)
	}
	rep.Missing = missing
	var drop []cas.Entry
	if err := store.Entries(func(e cas.Entry) error {
		if !keep[e.Digest] {
			drop = append(drop, e)
		}
		return nil
	}); err != nil {
		return rep, err
	}
	for _, e := range drop {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		if err := store.RemoveEntry(e); err != nil {
			return rep, err
		}
		if e.Logical {
			rep.Logical++
		} else {
			rep.Objects++
			rep.Bytes += e.Size
		}
	}
	return rep, nil
}
