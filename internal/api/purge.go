package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"
	"sort"

	"terva.sh/lampi/internal/catalog"
)

// PurgePlan is what Purge removes for one session. Objects and Logical
// are the CAS objects and logical indexes that no other session names.
// Kept counts the session's digests that another session still names.
type PurgePlan struct {
	Session   catalog.SessionInfo
	Artifacts int
	Objects   []string
	Logical   []string
	Kept      int
}

// PlanPurge works out what purging sessionUID removes. It reads the
// catalog and the CAS and writes nothing. ok is false when the session
// is not stored.
//
// The session names its artifact digests, the chunk and tail digests
// of its last manifest, the chunks of its logical files, and the tail
// blobs of each grown_from chain. A tail is recomputed from the head:
// the bytes between two recorded sizes of one path. A digest another
// session names, or a chunk of a logical file another session names,
// is kept. A tail or chunk list from an older manifest that no row
// records, and a blob put for a manifest that was never posted, are not
// found.
func (s *Server) PlanPurge(ctx context.Context, sessionUID string) (PurgePlan, bool, error) {
	info, ok, err := s.Catalog.Session(ctx, sessionUID)
	if err != nil || !ok {
		return PurgePlan{}, false, err
	}
	arts, err := s.Catalog.Artifacts(ctx, sessionUID)
	if err != nil {
		return PurgePlan{}, false, err
	}
	named := map[string]bool{}
	for _, a := range arts {
		named[a.SHA256] = true
	}
	for _, a := range info.Manifest.Artifacts {
		addNamed(named, a.SHA256, a.TailSHA256, a.ChunkSHA256s...)
	}
	for _, d := range s.tailDigests(arts) {
		named[d] = true
	}

	keep, err := s.Catalog.OtherDigests(ctx, sessionUID)
	if err != nil {
		return PurgePlan{}, false, err
	}
	others, err := s.Catalog.ListSessions(ctx)
	if err != nil {
		return PurgePlan{}, false, err
	}
	for _, o := range others {
		if o.UID == sessionUID {
			continue
		}
		for _, a := range o.Manifest.Artifacts {
			addNamed(keep, a.SHA256, a.TailSHA256, a.ChunkSHA256s...)
		}
	}
	for d := range keep {
		_, _, chunks, err := s.CAS.Stored(d)
		if err != nil {
			return PurgePlan{}, false, err
		}
		addNamed(keep, "", "", chunks...)
	}

	plan := PurgePlan{Session: info, Artifacts: len(arts)}
	var objects, logical []string
	// A chunk of the session's own logical file is named too. It is
	// found as the index is read, so this is a worklist.
	queue := make([]string, 0, len(named))
	for d := range named {
		queue = append(queue, d)
	}
	seen := map[string]bool{}
	for len(queue) > 0 {
		d := queue[0]
		queue = queue[1:]
		if seen[d] {
			continue
		}
		seen[d] = true
		if keep[d] {
			plan.Kept++
			continue
		}
		object, isLogical, chunks, err := s.CAS.Stored(d)
		if err != nil {
			return PurgePlan{}, false, err
		}
		if object {
			objects = append(objects, d)
		}
		if isLogical {
			logical = append(logical, d)
		}
		queue = append(queue, chunks...)
	}
	sort.Strings(objects)
	sort.Strings(logical)
	plan.Objects, plan.Logical = objects, logical
	return plan, true, nil
}

func addNamed(set map[string]bool, digest, tail string, chunks ...string) {
	for _, d := range append([]string{digest, tail}, chunks...) {
		if d != "" {
			set[d] = true
		}
	}
}

// tailDigests recomputes the tail blobs of each path's grown_from
// chain. A tail PUT was the bytes after the stored head, so it is the
// span of the newest bytes between two recorded sizes that are both
// prefixes of it. A path whose newest bytes cannot be read is skipped.
func (s *Server) tailDigests(arts []catalog.ArtifactRow) []string {
	byPath := map[string][]catalog.ArtifactRow{}
	for _, a := range arts {
		byPath[a.RelPath] = append(byPath[a.RelPath], a)
	}
	var out []string
	for _, rows := range byPath {
		if len(rows) < 2 {
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Size < rows[j].Size })
		out = append(out, s.spanDigests(rows)...)
	}
	return out
}

func (s *Server) spanDigests(rows []catalog.ArtifactRow) []string {
	newest := rows[len(rows)-1]
	r, err := s.CAS.Open(newest.SHA256)
	if err != nil {
		return nil
	}
	defer r.Close()
	full, span := sha256.New(), sha256.New()
	w := io.MultiWriter(full, span)
	var pos int64
	var out []string
	for _, row := range rows {
		if row.Size > pos {
			n, err := io.CopyN(w, r, row.Size-pos)
			pos += n
			if err != nil {
				// Short or unreadable: no later span can be checked.
				return out
			}
		}
		if hex.EncodeToString(full.Sum(nil)) != row.SHA256 {
			continue
		}
		out = append(out, hex.EncodeToString(span.Sum(nil)))
		span.Reset()
	}
	return out
}

// Purge removes what plan names: CAS objects and logical indexes
// first, then the session's derived files, then its catalog rows. A
// purge that stops part way can be run again; the catalog rows are the
// last thing to go. The caller holds lake.lock, so no worker or
// request is running.
func (s *Server) Purge(ctx context.Context, plan PurgePlan) error {
	for _, d := range append(append([]string(nil), plan.Objects...), plan.Logical...) {
		if err := s.CAS.Remove(d); err != nil {
			return err
		}
	}
	uid := plan.Session.UID
	if err := removeDerived(filepath.Join(s.Normalized, uid+".jsonl"), s.Parquet, uid); err != nil {
		return err
	}
	_, err := s.Catalog.DeleteSession(ctx, uid)
	return err
}
