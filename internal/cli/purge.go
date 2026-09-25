package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/lakelock"
)

const purgeUsage = `terva-lampi serve purge — remove one session from the lake

usage:
  terva-lampi serve purge --session UID [--data DIR] [--yes]

Lists what purge removes and changes nothing. --yes removes it: the
CAS objects and logical indexes no other session names, then the
session's normalized JSONL and parquet, then its catalog rows
(session, artifacts, provenance, aliases, normalize job).

The session's blobs are its artifact digests, the chunks and tail of
its last manifest, the chunks of its logical files, and the tail
blobs of each file that grew. A blob another session names is kept.
A tail or chunk list from an older manifest that the catalog did not
record is not found. A backup taken before the purge still holds the
bytes.

purge takes lake.lock, so it refuses while serve runs. Stop serve
first. An agent that still has the file uploads it again as a new
session unless its project is taken off the allowlist.
`

func runServePurge(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), purgeUsage)
		return nil
	}
	var data, uid string
	var yes bool
	rest, err := parseFlags(env, args, purgeUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&uid, "session", "", "session_uid to remove")
		fs.BoolVar(&yes, "yes", false, "remove; without it, only list")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), purgeUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if uid == "" {
		fmt.Fprint(env.stdout(), purgeUsage)
		return errors.New("serve purge needs --session")
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	lock, err := lakelock.Acquire(data)
	if err != nil {
		return fmt.Errorf("purge: %w; stop serve first", err)
	}
	defer lock.Release()
	lake, err := api.OpenIdle(data)
	if err != nil {
		return err
	}
	defer lake.Close()

	ctx := context.Background()
	plan, ok, err := lake.PlanPurge(ctx, uid)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("purge: no session %s", uid)
	}
	out := env.stdout()
	fmt.Fprintf(out, "session %s %s/%s\n", uid, plan.Session.Harness, plan.Session.NativeID)
	fmt.Fprintf(out, "catalog: the session and %d artifacts\n", plan.Artifacts)
	fmt.Fprintf(out, "objects: %d\n", len(plan.Objects))
	for _, d := range plan.Objects {
		fmt.Fprintf(out, "  %s\n", d)
	}
	fmt.Fprintf(out, "logical indexes: %d\n", len(plan.Logical))
	for _, d := range plan.Logical {
		fmt.Fprintf(out, "  %s\n", d)
	}
	fmt.Fprintf(out, "kept, named by another session: %d\n", plan.Kept)
	if !yes {
		fmt.Fprintln(out, "dry run; pass --yes to remove")
		return nil
	}
	if err := lake.Purge(ctx, plan); err != nil {
		return err
	}
	fmt.Fprintf(out, "purged %s\n", uid)
	return nil
}
