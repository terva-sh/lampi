package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
)

// CompactOptions tunes Compact.
type CompactOptions struct {
	// DryRun works out what would change and writes nothing.
	DryRun bool
	// MinAge spares an unreferenced entry written more recently than
	// this: a blob a client put for a manifest it has not posted yet.
	MinAge time.Duration
}

// CompactReport is what one Compact did, or in a dry run would do.
type CompactReport struct {
	// Paths counts files with more than one stored version, and
	// Versions the older versions checked against each file's newest.
	Paths    int
	Versions int
	// Folded counts whole copies made prefix records, and Flattened
	// the records pointed further along their chain.
	Folded    int
	Flattened int
	// Divergent counts branches after the first: a version that is not
	// a prefix of its file's newest, whose own older versions fold into
	// it instead.
	Divergent int
	// Looped counts folds refused because the base reads from the
	// version.
	Looped int
	// Unreadable lists newest versions that could not be read. Their
	// files are left as they are.
	Unreadable []string
	// Unreferenced counts objects and logical entries nothing names.
	Unreferenced int
	// Reclaimed is the bytes of object files removed.
	Reclaimed int64
}

// Compact makes the CAS hold each file's bytes once. Every older
// version of a file that is a prefix of its newest becomes a prefix
// record of it, and an existing record is pointed at it directly. Then
// objects and logical entries that no catalog row, manifest, or
// referenced record reads from are removed: tails already assembled,
// and the chunks of folded chunk lists.
//
// Every fold is checked first: the file's newest version is read once,
// and each older version's digest is compared with the hash of that
// many leading bytes. It is safe to run again, and a second run over
// a compacted lake changes nothing. The caller holds lake.lock unless
// opt.DryRun is set.
func (s *Server) Compact(ctx context.Context, opt CompactOptions) (CompactReport, error) {
	var rep CompactReport
	sessions, err := s.Catalog.ListSessions(ctx)
	if err != nil {
		return rep, err
	}
	// plan maps a version to the newest version of its file, which it
	// is a verified prefix of.
	plan := map[string]fold{}
	for _, info := range sessions {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		arts, err := s.Catalog.Artifacts(ctx, info.UID)
		if err != nil {
			return rep, err
		}
		for _, rows := range versionsByPath(arts) {
			rep.Paths++
			s.planPath(rows, plan, &rep)
		}
	}
	bases, err := s.foldBases(plan)
	if err != nil {
		return rep, err
	}
	digests := make([]string, 0, len(plan))
	for d := range plan {
		digests = append(digests, d)
	}
	sort.Strings(digests)
	// A fold can clear the way for another: a base that read from a
	// version stops doing so once something between them is folded. So
	// each pass checks for a loop against the store with the folds made
	// so far laid over it, the same graph in a dry run and a real one,
	// and refused folds are tried again until a pass folds nothing new.
	applied := map[string]string{}
	pending := digests
	for len(pending) > 0 {
		var refused []string
		for _, d := range pending {
			loops, err := s.readsFrom(bases[d], d, applied)
			if err != nil {
				return rep, err
			}
			if loops {
				refused = append(refused, d)
				continue
			}
			folded, err := s.applyFold(d, bases[d], plan[d].length, opt, &rep)
			if err != nil {
				return rep, err
			}
			if !folded {
				refused = append(refused, d)
				continue
			}
			applied[d] = bases[d]
		}
		if len(refused) == len(pending) {
			rep.Looped += len(refused)
			for _, d := range refused {
				// Left as it is, and the sweep must see it that way.
				delete(bases, d)
			}
			break
		}
		pending = refused
	}

	if err := s.sweepUnreferenced(ctx, sessions, bases, opt, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

type fold struct {
	newest string
	length int64
}

// versionsByPath groups a session's artifact rows by relpath, one row
// per digest, smallest first. A path with one version is left out.
func versionsByPath(arts []catalog.ArtifactRow) [][]catalog.ArtifactRow {
	byPath := map[string][]catalog.ArtifactRow{}
	seen := map[string]bool{}
	for _, a := range arts {
		key := a.RelPath + "\x00" + a.SHA256
		if seen[key] {
			continue
		}
		seen[key] = true
		byPath[a.RelPath] = append(byPath[a.RelPath], a)
	}
	paths := make([]string, 0, len(byPath))
	for p, rows := range byPath {
		if len(rows) > 1 {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	out := make([][]catalog.ArtifactRow, 0, len(paths))
	for _, p := range paths {
		rows := byPath[p]
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Size != rows[j].Size {
				return rows[i].Size < rows[j].Size
			}
			return rows[i].SHA256 < rows[j].SHA256
		})
		out = append(out, rows)
	}
	return out
}

// planPath adds to plan every version of one file that is a prefix of
// a longer one. The longest version is read once, and each shorter one
// that is its prefix is planned against it. What is left, a divergent
// branch, is treated the same way, so each branch folds into its own
// newest.
func (s *Server) planPath(rows []catalog.ArtifactRow, plan map[string]fold, rep *CompactReport) {
	rep.Versions += len(rows) - 1
	for first := true; len(rows) > 1; first = false {
		newest := rows[len(rows)-1]
		older := rows[:len(rows)-1]
		if !first {
			rep.Divergent++
		}
		prefixes, err := s.prefixesOf(newest.SHA256, older)
		if err != nil {
			rep.Unreadable = append(rep.Unreadable, newest.SHA256)
			rows = older
			continue
		}
		var rest []catalog.ArtifactRow
		for _, row := range older {
			if !prefixes[row.SHA256] {
				rest = append(rest, row)
				continue
			}
			if _, ok := plan[row.SHA256]; !ok {
				plan[row.SHA256] = fold{newest: newest.SHA256, length: row.Size}
			}
		}
		rows = rest
	}
}

// prefixesOf reads newest once and returns the rows whose digest is the
// hash of that many of its leading bytes. rows are smallest first.
func (s *Server) prefixesOf(newest string, rows []catalog.ArtifactRow) (map[string]bool, error) {
	r, err := s.CAS.Open(newest)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	h := sha256.New()
	var pos int64
	out := map[string]bool{}
	for _, row := range rows {
		if row.Size <= 0 {
			continue
		}
		if row.Size > pos {
			n, err := io.CopyN(h, r, row.Size-pos)
			pos += n
			if errors.Is(err, io.EOF) {
				// The newest is shorter than this row, so it is not a
				// prefix, and nor is anything longer.
				return out, nil
			}
			if err != nil {
				return nil, err
			}
		}
		// Sum does not reset the hash, so the pass carries on.
		if hex.EncodeToString(h.Sum(nil)) == row.SHA256 {
			out[row.SHA256] = true
		}
	}
	// Read to the end, so a newest that fails partway is unreadable,
	// not a source of records.
	if _, err := io.Copy(io.Discard, r); err != nil {
		return nil, err
	}
	return out, nil
}

// foldBases is where each planned version's record should point: its
// newest version, followed through versions that are folded too and
// through existing records, to a file that stays whole.
func (s *Server) foldBases(plan map[string]fold) (map[string]string, error) {
	bases := make(map[string]string, len(plan))
	for d, f := range plan {
		// Each step is to a longer file, so a chain ends. One that
		// comes back on itself is damage: point at the newest as read,
		// and Fold refuses the loop.
		base := f.newest
		seen := map[string]bool{d: true}
		for {
			if seen[base] {
				base = f.newest
				break
			}
			seen[base] = true
			if next, ok := plan[base]; ok {
				base = next.newest
				continue
			}
			t, err := s.CAS.Terminal(base)
			if err != nil {
				return nil, err
			}
			if t == base {
				break
			}
			base = t
		}
		bases[d] = base
	}
	return bases, nil
}

// applyFold makes d a prefix record of base, counting what that
// changes. A record already pointing at base is left alone. The caller
// has found that base does not read from d. folded is false when the
// store refuses the fold anyway.
func (s *Server) applyFold(d, base string, length int64, opt CompactOptions, rep *CompactReport) (folded bool, err error) {
	size, object, err := s.CAS.ObjectSize(d)
	if err != nil {
		return false, err
	}
	cur, _, isPrefix, err := s.CAS.PrefixOf(d)
	if err != nil {
		return false, err
	}
	if isPrefix && cur == base && !object {
		return true, nil
	}
	if opt.DryRun {
		countFold(rep, isPrefix, object, size)
		return true, nil
	}
	freed, err := s.CAS.Fold(d, base, length)
	if errors.Is(err, cas.ErrWouldLoop) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	countFold(rep, isPrefix, object, freed)
	return true, nil
}

// readsFrom reports whether reading from reaches target, through chunk
// lists and prefix records, with the folds in applied standing in for
// what those digests read from now.
func (s *Server) readsFrom(from, target string, applied map[string]string) (bool, error) {
	seen := map[string]bool{}
	queue := []string{from}
	for len(queue) > 0 {
		d := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if d == target {
			return true, nil
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		if base, ok := applied[d]; ok {
			queue = append(queue, base)
			continue
		}
		object, _, links, err := s.CAS.Stored(d)
		if err != nil {
			return false, err
		}
		if object {
			// Open reads the object and nothing it might also have
			// a record for.
			continue
		}
		queue = append(queue, links...)
	}
	return false, nil
}

func countFold(rep *CompactReport, wasPrefix, object bool, freed int64) {
	if wasPrefix && !object {
		rep.Flattened++
	} else {
		rep.Folded++
	}
	rep.Reclaimed += freed
}

// sweepUnreferenced removes objects and logical entries that nothing
// reads from, once they are older than opt.MinAge. What is kept starts
// at every digest the catalog and the last manifests name, and follows
// chunk lists and prefix records down. In a dry run the planned folds
// stand in for the records they would write.
func (s *Server) sweepUnreferenced(ctx context.Context, sessions []catalog.SessionInfo, bases map[string]string, opt CompactOptions, rep *CompactReport) error {
	named, err := s.Catalog.ReferencedDigests(ctx)
	if err != nil {
		return err
	}
	for _, info := range sessions {
		for _, a := range info.Manifest.Artifacts {
			addNamed(named, a.SHA256, a.TailSHA256, a.ChunkSHA256s...)
		}
	}
	keep := make(map[string]bool, len(named))
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
		if base, ok := bases[d]; ok && opt.DryRun {
			queue = append(queue, base)
			continue
		}
		_, isLogical, links, err := s.CAS.Stored(d)
		if err != nil {
			return err
		}
		// A logical entry that does not parse hides what it reads
		// from. Stop rather than remove what it needs.
		if isLogical && len(links) == 0 {
			return fmt.Errorf("compact: logical index %s is unreadable; run serve fsck", d)
		}
		queue = append(queue, links...)
	}

	cutoff := time.Now().Add(-opt.MinAge)
	var drop []cas.Entry
	err = s.CAS.Entries(func(e cas.Entry) error {
		if keep[e.Digest] || !e.Modified.Before(cutoff) {
			return nil
		}
		drop = append(drop, e)
		return nil
	})
	if err != nil {
		return err
	}
	for _, e := range drop {
		if !opt.DryRun {
			if err := s.CAS.RemoveEntry(e); err != nil {
				return err
			}
		}
		rep.Unreferenced++
		if !e.Logical {
			rep.Reclaimed += e.Size
		}
	}
	return nil
}
