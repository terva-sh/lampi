package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
)

const serveNormalizeUsage = `terva-lampi serve normalize — run normalization again

usage:
  terva-lampi serve normalize [--all] [--stale] [--failed] [--session UID]... [--dry-run] [--data DIR]
  terva-lampi serve normalize --status [--json] [--data DIR]

Queues sessions to be normalized again, the same job an upload queues.
--stale takes every session the dashboard counts as unknown: it has no
job and no published result for its current head, as sessions ingested
before normalization generations were tracked are. --failed takes every
session whose last normalization failed, for example after an upgrade
fixed the cause. --session names one, and may be repeated. --all takes
every session, to rewrite the derived files after an upgrade changes
how they are written, as the parquet codec did. At least one is
required. --dry-run prints what would be queued and queues nothing.

Each session is projected again from its raw blobs, two at a time.
A viewer reading a session while it is replaced is asked to reload.

--status queues nothing. It reads the catalog, so it works with serve
stopped. It prints sessions by state, the jobs outstanding and how
long ago the oldest was queued, and each failed session with its
message. A job stays in the table while serve runs it, until its
result is stored, so outstanding is queued or running. --json prints
the normalization object of GET /v1/stats instead. queued, running and
retrying are 0 there: they are what a serve process holds, and this
does not ask one.

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
	var all, stale, failed, dry, status, asJSON bool
	var named sessionFlags
	rest, err := parseFlags(env, args, serveNormalizeUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.BoolVar(&all, "all", false, "every session")
		fs.BoolVar(&stale, "stale", false, "sessions with no job and no current result")
		fs.BoolVar(&failed, "failed", false, "sessions whose normalization failed")
		fs.Var(&named, "session", "a session UID, repeatable")
		fs.BoolVar(&dry, "dry-run", false, "print what would be queued")
		fs.BoolVar(&status, "status", false, "print normalization status and queue nothing")
		fs.BoolVar(&asJSON, "json", false, "with --status, print the /v1/stats normalization object")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), serveNormalizeUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if status {
		if all || stale || failed || dry || len(named) > 0 {
			return fmt.Errorf("serve normalize --status takes no selector and no --dry-run")
		}
		if data, err = lakeDir(env, data); err != nil {
			return err
		}
		return normalizeStatus(env, data, asJSON)
	}
	if asJSON {
		return fmt.Errorf("--json goes with --status")
	}
	if !all && !stale && !failed && len(named) == 0 {
		fmt.Fprint(env.stdout(), serveNormalizeUsage)
		return fmt.Errorf("serve normalize needs --all, --stale, --failed or --session")
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
	if all {
		uids, states, err := cat.NormalizationStates(ctx)
		if err != nil {
			return err
		}
		for i, uid := range uids {
			add(uid, states[i])
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

// normalizeStatus prints the lake's normalization from its catalog.
func normalizeStatus(env Env, data string, asJSON bool) error {
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve normalize: %w", err)
	}
	cat, err := catalog.OpenReadOnly(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	ctx := context.Background()
	st, err := api.CatalogNormalization(ctx, cat, time.Now())
	if err != nil {
		return err
	}
	out := env.stdout()
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	}
	fmt.Fprintf(out, "sessions: ready=%d pending=%d failed=%d unknown=%d\n",
		st.Sessions["ready"], st.Sessions["pending"], st.Sessions["failed"], st.Sessions["unknown"])
	fmt.Fprintf(out, "jobs: %d outstanding, queued or running, oldest queued %s ago\n", st.Jobs,
		ageString(st.OldestPendingSeconds))
	uids, err := cat.SessionsInNormalizationState(ctx, "failed")
	if err != nil {
		return err
	}
	for _, uid := range uids {
		info, ok, err := cat.Session(ctx, uid)
		if err != nil {
			return err
		}
		if ok {
			fmt.Fprintf(out, "failed %s: %s\n", uid, info.NormalizeError)
		}
	}
	return nil
}
