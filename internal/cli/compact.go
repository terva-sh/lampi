package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/recall"
)

const compactUsage = `terva-lampi serve compact — store each grown file's bytes once

usage:
  terva-lampi serve compact [--data DIR] [--dry-run] [--min-age 1h]

A transcript that grows keeps every version readable under its own
digest. compact makes each older version of a file that is a prefix of
its newest a record of that many of the newest's bytes, and points an
existing record straight at the newest. A version that is not a prefix
of the newest starts a branch of its own, and the versions before it
fold into it the same way. It then removes the objects and
logical entries that nothing reads from: tails already assembled into a
file, and the chunks of lists it folded. An unreferenced entry written
within --min-age is kept, since it may be a blob put for a manifest not
yet posted.

Before any fold, the newest version of each file is read once and each
older version's digest is checked against the hash of that many
leading bytes.

Last, every object stored raw by a release from before objects were
compressed is rewritten as a zstd frame. Its bytes are hashed on the
way, and one that is not its digest is left for serve fsck.

Then, when the lake has a search index, compact merges its full-text
index into one segment. That frees the entries of deleted rows, which
the index's own merges leave behind, and returns the space to the
filesystem. It rewrites the whole full-text index in one transaction,
so it needs free space of about the size of search.db while it runs.

compact can be run again, and a second run over a compacted lake changes
nothing.

--dry-run reports what would change and writes nothing. It can run
while serve runs. Without it, compact takes lake.lock, so stop serve
first. Take a backup before the first compact of a lake: an older
terva-lampi cannot read the records or the frames.
`

func runServeCompact(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), compactUsage)
		return nil
	}
	var data string
	var dryRun bool
	var minAge time.Duration
	rest, err := parseFlags(env, args, compactUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.BoolVar(&dryRun, "dry-run", false, "report what would change and write nothing")
		fs.DurationVar(&minAge, "min-age", time.Hour, "keep unreferenced entries newer than this")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), compactUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if minAge < 0 {
		return fmt.Errorf("--min-age %s is negative", minAge)
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	if !dryRun {
		lock, err := lockLake("compact", data, "--dry-run")
		if err != nil {
			return err
		}
		defer lock.Release()
	} else if err := requireCurrentSchema(data); err != nil {
		// A dry run does not take the lock, and opening the lake
		// would migrate the catalog under a running serve.
		return fmt.Errorf("compact --dry-run: %w", err)
	}
	lake, err := api.OpenIdle(data)
	if err != nil {
		return err
	}
	defer lake.Close()

	rep, err := lake.Compact(context.Background(), api.CompactOptions{DryRun: dryRun, MinAge: minAge})
	out := env.stdout()
	fmt.Fprintf(out, "files with more than one version: %d, older versions checked: %d\n", rep.Paths, rep.Versions)
	fmt.Fprintf(out, "folded into a prefix record: %d\n", rep.Folded)
	fmt.Fprintf(out, "records pointed at the newest: %d\n", rep.Flattened)
	fmt.Fprintf(out, "divergent branches, folded into their own newest: %d\n", rep.Divergent)
	if rep.Looped > 0 {
		fmt.Fprintf(out, "refused, the record would loop: %d\n", rep.Looped)
	}
	fmt.Fprintf(out, "unreadable newest versions, left: %d\n", len(rep.Unreadable))
	for _, d := range rep.Unreadable {
		fmt.Fprintf(out, "  %s\n", d)
	}
	fmt.Fprintf(out, "unreferenced entries: %d\n", rep.Unreferenced)
	fmt.Fprintf(out, "object bytes reclaimed: %d (%.1f MiB)\n", rep.Reclaimed, float64(rep.Reclaimed)/(1<<20))
	if dryRun {
		fmt.Fprintf(out, "raw objects to compress: %d, %.1f MiB\n", rep.Reencoded, float64(rep.RawBytes)/(1<<20))
	} else {
		fmt.Fprintf(out, "raw objects compressed: %d, %.1f MiB to %.1f MiB\n", rep.Reencoded, float64(rep.RawBytes)/(1<<20), float64(rep.CompressedBytes)/(1<<20))
	}
	if len(rep.Damaged) > 0 {
		fmt.Fprintf(out, "raw objects that are not their digest, left for fsck: %d\n", len(rep.Damaged))
		for _, d := range rep.Damaged {
			fmt.Fprintf(out, "  %s\n", d)
		}
	}
	if err != nil {
		return err
	}
	index := filepath.Join(data, recall.IndexFile)
	if dryRun {
		if _, err := os.Stat(index); err == nil {
			fmt.Fprintf(out, "search index to optimize: %.1f MiB\n", float64(recall.IndexSize(index))/(1<<20))
		}
		fmt.Fprintln(out, "dry run; nothing was written")
		return nil
	}
	before, after, err := recall.OptimizeIndex(context.Background(), index)
	if before > 0 {
		fmt.Fprintf(out, "search index optimized: %.1f MiB to %.1f MiB\n", float64(before)/(1<<20), float64(after)/(1<<20))
	}
	return err
}
