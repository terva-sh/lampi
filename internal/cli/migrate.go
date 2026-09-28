package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/lakelock"
)

const migrateUsage = `terva-lampi serve migrate — upgrade the catalog schema

usage:
  terva-lampi serve migrate [--check] [--data DIR]

serve upgrades the catalog when it starts, so this is only needed to
upgrade without starting the listener: a Kubernetes init container, or
a step before switching to a new image.

Without --check it takes lake.lock, so stop serve first. A catalog with
data is copied to migration-backups/ in the lake directory before the
first step, and the newest three copies are kept. A copy that fails
stops the upgrade. Each step prints one line.

--check reads the schema version without writing or taking the lock,
so it runs beside serve. It prints the file's version and this
binary's, and exits 0 whether or not an upgrade is pending. A catalog
newer than this binary is an error: that binary cannot run the lake.

To roll back an upgrade, stop serve, copy the newest file in
migration-backups/ (or catalog.db from a serve backup taken before it)
over catalog.db, and start the older version.
`

func runServeMigrate(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), migrateUsage)
		return nil
	}
	var data string
	var check bool
	rest, err := parseFlags(env, args, migrateUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.BoolVar(&check, "check", false, "report the versions and change nothing")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), migrateUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no catalog; serve makes one on its first start", data)
	} else if err != nil {
		return err
	}
	want := catalog.SchemaVersion()
	if check {
		v, err := catalog.FileVersion(path)
		if err != nil {
			return err
		}
		switch {
		case v > want:
			return fmt.Errorf("catalog schema %d is newer than this binary's %d; run a newer terva-lampi", v, want)
		case v == want:
			fmt.Fprintf(env.stdout(), "catalog schema %d, up to date\n", v)
		default:
			fmt.Fprintf(env.stdout(), "catalog schema %d, this binary writes %d: %d migrations pending\n", v, want, want-v)
		}
		return nil
	}
	lock, err := lakelock.Acquire(data)
	if err != nil {
		return fmt.Errorf("serve migrate: %w; stop serve first, or pass --check", err)
	}
	defer lock.Release()
	cat, err := catalog.Open(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	printMigration(env.stdout(), "", cat.Migrated())
	return nil
}

// printMigration writes the schema line and one line per step. serve
// prefixes its lines; serve migrate does not.
func printMigration(w io.Writer, prefix string, m catalog.Migration) {
	if m.From == m.To {
		fmt.Fprintf(w, "%scatalog schema %d, up to date\n", prefix, m.To)
		return
	}
	if m.Created {
		fmt.Fprintf(w, "%scatalog schema %d, created\n", prefix, m.To)
		return
	}
	if m.Backup != "" {
		fmt.Fprintf(w, "%scatalog backup before migrating: %s\n", prefix, m.Backup)
	}
	for i, step := range m.Steps {
		fmt.Fprintf(w, "%scatalog migration %d: %s\n", prefix, m.From+i+1, step)
	}
	fmt.Fprintf(w, "%scatalog schema %d -> %d\n", prefix, m.From, m.To)
}

// requireCurrentSchema refuses a lake whose catalog this binary would
// migrate, for a command that opens it for writing without lake.lock.
// A lake with no catalog yet passes.
func requireCurrentSchema(data string) error {
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	v, err := catalog.FileVersion(path)
	if err != nil {
		return err
	}
	if want := catalog.SchemaVersion(); v != want {
		return fmt.Errorf("catalog schema %d, this binary writes %d; stop serve and run terva-lampi serve migrate first, or use the terva-lampi that serve runs", v, want)
	}
	return nil
}
