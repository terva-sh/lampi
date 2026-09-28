package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

const profilesUsage = `terva-lampi serve profiles — manage the profiles agents fetch

usage:
  terva-lampi serve profiles [list] [--data DIR]
  terva-lampi serve profiles show NAME [--revisions] [--data DIR]
  terva-lampi serve profiles set NAME FILE [--note TEXT] [--data DIR]
  terva-lampi serve profiles delete NAME [--note TEXT] [--data DIR]
  terva-lampi serve profiles import FILE [--note TEXT] [--data DIR]

Profiles live in the lake's catalog. Each save is kept as a revision
with who made it and an optional note saying why.

list prints one line per profile: name, version, revision, and who
changed it last. show prints a profile's document, or with --revisions
its history. set replaces one profile with the document in FILE, or
stdin when FILE is -. delete removes a profile; default cannot be
deleted, nor can a profile a device uses. import reads a profiles file,
{"profiles": {"NAME": {...}}}, and saves each profile in it. Every
profile is checked before any is saved. A profile whose document has
not changed is left alone.

serve does not read profiles.json or --profiles: import the file.
Agents pick up a change at their next profile fetch. All five run while
serve runs, and the writes append to audit.jsonl in the lake directory.
`

func runServeProfiles(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), profilesUsage)
		return nil
	}
	sub := "list"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		sub, args = args[0], args[1:]
	}
	need := map[string]int{"list": 0, "show": 1, "set": 2, "delete": 1, "import": 1}
	n, ok := need[sub]
	if !ok {
		fmt.Fprint(env.stdout(), profilesUsage)
		return fmt.Errorf("unknown serve profiles command %q", sub)
	}
	var pos []string
	for len(pos) < n {
		// "-" is stdin for set, not a flag.
		if len(args) == 0 || args[0] == "" || (args[0][0] == '-' && args[0] != "-") {
			fmt.Fprint(env.stdout(), profilesUsage)
			return fmt.Errorf("serve profiles %s needs %d argument(s)", sub, n)
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	var data, note string
	var revisions bool
	rest, err := parseFlags(env, args, profilesUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&note, "note", "", "why, kept with the revision")
		fs.BoolVar(&revisions, "revisions", false, "show a profile's history")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), profilesUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve profiles: %w", err)
	}
	ctx := context.Background()
	if sub == "list" || sub == "show" {
		cat, err := catalog.OpenReadOnly(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		if sub == "list" {
			return listProfiles(ctx, env, cat)
		}
		return showProfile(ctx, env, cat, pos[0], revisions)
	}
	cat, err := catalog.Open(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	actor := "serve profiles " + sub
	now := time.Now()
	switch sub {
	case "set":
		raw, err := readProfileDoc(env, pos[1])
		if err != nil {
			return err
		}
		p, changed, err := cat.PutProfile(ctx, pos[0], raw, actor, note, now)
		if err != nil {
			return err
		}
		// An unchanged set still flushes below: a line an earlier
		// write left queued goes out now.
		if changed {
			fmt.Fprintf(env.stdout(), "set %s revision %d version %s\n", p.Name, p.Revision, p.Version)
		} else {
			fmt.Fprintf(env.stdout(), "unchanged %s version %s\n", p.Name, p.Version)
		}
	case "delete":
		rev, err := cat.DeleteProfile(ctx, pos[0], actor, note, now)
		if errors.Is(err, catalog.ErrNoProfile) {
			return fmt.Errorf("no profile named %s; serve profiles list shows them", pos[0])
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "deleted %s revision %d\n", rev.Profile, rev.ID)
	case "import":
		if err := importProfiles(ctx, env, cat, pos[0], actor, note, now); err != nil {
			// Profiles saved before the failure stand; their lines go
			// out with the error.
			if ferr := cat.FlushAudit(ctx, data); ferr != nil {
				return errors.Join(err, fmt.Errorf("writing %s: %w; the lines stay queued in the catalog", audit.FileName, ferr))
			}
			return err
		}
	}
	// The changes are committed. A line that cannot be written now stays
	// queued, and the next serve or serve command writes it.
	if err := cat.FlushAudit(ctx, data); err != nil {
		return fmt.Errorf("the change stands, but writing it to %s failed: %w; the line stays queued in the catalog until the audit log can be written", audit.FileName, err)
	}
	return nil
}

func listProfiles(ctx context.Context, env Env, cat *catalog.Catalog) error {
	list, err := cat.Profiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range list {
		fmt.Fprintf(env.stdout(), "%s version=%s revision=%d updated=%s by=%s\n",
			p.Name, p.Version, p.Revision, p.Updated.UTC().Format(time.RFC3339), p.UpdatedBy)
	}
	if len(list) == 0 {
		fmt.Fprintln(env.stdout(), "no profiles; devices get an empty default profile. serve profiles import FILE adds some")
	}
	return nil
}

func showProfile(ctx context.Context, env Env, cat *catalog.Catalog, name string, revisions bool) error {
	if revisions {
		revs, err := cat.ProfileRevisions(ctx, name)
		if err != nil {
			return err
		}
		if len(revs) == 0 {
			return fmt.Errorf("no profile named %s has a revision", name)
		}
		for _, r := range revs {
			what := "version=" + r.Version
			if r.Deleted {
				what = "deleted"
			}
			fmt.Fprintf(env.stdout(), "revision=%d %s at=%s by=%s", r.ID, what, r.Created.UTC().Format(time.RFC3339), r.CreatedBy)
			if r.Note != "" {
				fmt.Fprintf(env.stdout(), " note=%q", r.Note)
			}
			fmt.Fprintln(env.stdout())
		}
		return nil
	}
	p, err := cat.ProfileByName(ctx, name)
	if errors.Is(err, catalog.ErrNoProfile) {
		return fmt.Errorf("no profile named %s; serve profiles list shows them", name)
	}
	if err != nil {
		return err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(p.Document), "", "  "); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "%s\n", out.Bytes())
	return nil
}

// maxProfileDoc caps a profile document read from stdin.
const maxProfileDoc = 1 << 20

// readProfileDoc reads one profile document from path, or stdin for -.
// Stdin past maxProfileDoc is refused, not cut: a cut document could
// still parse as a different one.
func readProfileDoc(env Env, path string) ([]byte, error) {
	if path == "-" {
		raw, err := io.ReadAll(io.LimitReader(env.stdin(), maxProfileDoc+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > maxProfileDoc {
			return nil, fmt.Errorf("serve profiles set: stdin is over %d bytes", maxProfileDoc)
		}
		return raw, nil
	}
	return os.ReadFile(path)
}

// importProfiles saves each profile in the profiles file at path. Every
// profile is checked first, so a file with one bad profile saves none.
func importProfiles(ctx context.Context, env Env, cat *catalog.Catalog, path, actor, note string, now time.Time) error {
	docs, err := config.ReadProfilesFile(path)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return fmt.Errorf("%s holds no profiles", path)
	}
	names := make([]string, 0, len(docs))
	for name := range docs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !config.ValidProfileName(name) {
			return fmt.Errorf("%s: %q: %w", path, name, catalog.ErrProfileName)
		}
		if _, err := config.ParseProfile(docs[name]); err != nil {
			return fmt.Errorf("%s: %s: %w; nothing was imported", path, name, err)
		}
	}
	for _, name := range names {
		p, changed, err := cat.PutProfile(ctx, name, docs[name], actor, note, now)
		if err != nil {
			return fmt.Errorf("%s: %s: %w; the profiles listed above were saved, the rest were not", path, name, err)
		}
		if changed {
			fmt.Fprintf(env.stdout(), "imported %s revision %d version %s\n", p.Name, p.Revision, p.Version)
		} else {
			fmt.Fprintf(env.stdout(), "unchanged %s version %s\n", p.Name, p.Version)
		}
	}
	return nil
}

// warnProfilesFile says loudly that a profiles file serve can see is
// not in force: --profiles, or profiles.json in the lake directory.
// serve calls it at start and on each SIGHUP, the moments an operator
// who edited the file expects it to apply.
func warnProfilesFile(env Env, data, flagPath string) {
	var paths []string
	if flagPath != "" {
		paths = append(paths, flagPath)
	}
	def := filepath.Join(data, config.ProfilesFileName)
	if _, err := os.Stat(def); err == nil && def != flagPath {
		paths = append(paths, def)
	}
	for _, p := range paths {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: WARNING: %s is NOT in force. Profiles live in the catalog, and serve does not read this file. Run: terva-lampi serve profiles import %s --data %s\n", p, shellQuote(p), shellQuote(data))
	}
}

// shellQuote makes s one POSIX shell word, so a suggested command
// can be copied as printed. A word of only safe characters is left as
// it is.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-+=:,@%") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
