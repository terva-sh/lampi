package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/catalog"
)

const serveNormalizeUsage = `terva-lampi serve normalize — run normalization again

usage:
  terva-lampi serve normalize [--stale] [--failed] [--session UID]... [--dry-run] [--data DIR]

Queues sessions to be normalized again, the same job an upload queues.
--stale takes every session the dashboard counts as unknown: it has no
job and no published result for its current head, as sessions ingested
before normalization generations were tracked are. --failed takes every
session whose last normalization failed, for example after an upgrade
fixed the cause. --session names one, and may be repeated. At least one
is required. --dry-run prints what would be queued and queues nothing.

The jobs are rows in the catalog, so this runs while serve runs. A
running serve starts them on its next SIGHUP (systemctl kill -s HUP
terva-lampi-serve, or systemctl reload where the unit has ExecReload),
and any serve starts them when it starts. The dashboard overview's normalization counts show them move
from pending to ready or failed. A failure is recorded on the session
as before.
`

// sessionFlags collects repeated --session values.
type sessionFlags []string

func (s *sessionFlags) String() string     { return fmt.Sprint(*s) }
func (s *sessionFlags) Set(v string) error { *s = append(*s, v); return nil }

func runServeNormalize(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), serveNormalizeUsage)
		return nil
	}
	var data string
	var stale, failed, dry bool
	var named sessionFlags
	rest, err := parseFlags(env, args, serveNormalizeUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.BoolVar(&stale, "stale", false, "sessions with no job and no current result")
		fs.BoolVar(&failed, "failed", false, "sessions whose normalization failed")
		fs.Var(&named, "session", "a session UID, repeatable")
		fs.BoolVar(&dry, "dry-run", false, "print what would be queued")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), serveNormalizeUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if !stale && !failed && len(named) == 0 {
		fmt.Fprint(env.stdout(), serveNormalizeUsage)
		return fmt.Errorf("serve normalize needs --stale, --failed or --session")
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve normalize: %w", err)
	}
	cat, err := catalog.Open(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	ctx := context.Background()

	type pick struct{ uid, why string }
	var picks []pick
	seen := map[string]bool{}
	add := func(uid, why string) {
		if !seen[uid] {
			seen[uid] = true
			picks = append(picks, pick{uid, why})
		}
	}
	for _, sel := range []struct {
		on    bool
		state string
	}{{stale, "unknown"}, {failed, "failed"}} {
		if !sel.on {
			continue
		}
		uids, err := cat.SessionsInNormalizationState(ctx, sel.state)
		if err != nil {
			return err
		}
		for _, uid := range uids {
			add(uid, sel.state)
		}
	}
	for _, uid := range named {
		state, err := cat.NormalizationState(ctx, uid)
		if err != nil {
			return fmt.Errorf("session %s: %w", uid, err)
		}
		add(uid, state)
	}

	verb := "queued"
	if dry {
		verb = "would queue"
	}
	counts := map[string]int{}
	for _, p := range picks {
		if !dry {
			if _, err := cat.EnqueueNormalize(ctx, p.uid, time.Now()); err != nil {
				return fmt.Errorf("session %s: %w", p.uid, err)
			}
		}
		counts[p.why]++
		fmt.Fprintf(env.stdout(), "%s %s (was %s)\n", verb, p.uid, p.why)
	}
	fmt.Fprintf(env.stdout(), "%s %d sessions: %d unknown, %d failed, %d other\n",
		verb, len(picks), counts["unknown"], counts["failed"], len(picks)-counts["unknown"]-counts["failed"])
	if !dry && len(picks) > 0 {
		fmt.Fprintln(env.stdout(), "a running serve starts them on SIGHUP (systemctl kill -s HUP terva-lampi-serve); any serve starts them when it starts")
	}
	return nil
}
