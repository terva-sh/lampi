package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakelock"
)

const backupUsage = `terva-lampi serve backup — copy the lake to a directory

usage:
  terva-lampi serve backup --out DIR [--data DIR] [--token-file PATH]

Writes DIR/catalog.db, then DIR/cas/sha256 and DIR/cas/logical, then
DIR/identity.json, then the token file. It runs while serve runs. The catalog is a VACUUM
INTO copy, one consistent snapshot. The CAS is copied after it, so
the copy holds every object that snapshot names. Upload temp files
and cas/partial are left out. An object already in DIR with the same
size is not copied again, so a second backup into DIR copies only
what is new. normalized/ and parquet/ are derived and are not copied.

identity.json holds the lake's private signing keys. Agents pin them,
and serve refuses to start on a catalog whose identity.json is lost,
so a restore needs this copy. When the catalog copy records a lake id
and identity.json is missing or names another lake, the backup fails.

--token-file is the file or directory serve reads. It is copied to
DIR under its own name. The copy holds sha256 lines, not tokens,
once serve has rewritten it.

The backup is the lake in plaintext. Keep DIR on encrypted storage.
`

const fsckUsage = `terva-lampi serve fsck — re-hash every stored object

usage:
  terva-lampi serve fsck [--data DIR] [--repair]

Reads every object under cas/sha256 and checks that its bytes hash to
its name. Reads identity.json and checks each key against its id and
the file against the lake id the catalog recorded. A missing file is a
failure only when the catalog recorded one. Reads every cas/logical index and checks that each chunk it
names is stored. Each bad entry is named on stdout, and the command
exits non-zero when there is one. It runs while serve runs.

--repair removes each bad object, and each index that does not parse.
The lake then reports that digest missing, and the next upload of that
file puts it again. An index whose chunk is missing is kept; the chunk's
own upload restores it. --repair takes lake.lock, so it refuses while
serve runs. Stop serve first.
`

func lakeDir(env Env, data string) (string, error) {
	if data != "" {
		return data, nil
	}
	return config.StateDir(env.getenv)
}

func runServeBackup(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), backupUsage)
		return nil
	}
	var data, out, tokenFile string
	rest, err := parseFlags(env, args, backupUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&out, "out", "", "backup directory")
		fs.StringVar(&tokenFile, "token-file", "", "device token file or directory to copy")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), backupUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if out == "" {
		fmt.Fprint(env.stdout(), backupUsage)
		return errors.New("serve backup needs --out")
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	if err := refuseInsideLake(data, out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o700); err != nil {
		return err
	}

	sessions, err := backupCatalog(filepath.Join(data, "catalog.db"), filepath.Join(out, "catalog.db"))
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "catalog.db: %d sessions\n", sessions)

	store := &cas.Store{Root: filepath.Join(data, "cas")}
	copied, err := store.Backup(filepath.Join(out, "cas"))
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "cas: %d new entries copied\n", copied)

	if err := backupIdentity(env, data, out); err != nil {
		return err
	}

	if tokenFile != "" {
		dst := filepath.Join(out, filepath.Base(filepath.Clean(tokenFile)))
		if err := copyTokens(tokenFile, dst); err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "token file: %s\n", dst)
	}
	return nil
}

// refuseInsideLake rejects a backup into the lake itself: the copy
// would be walked while it is written, and the lake would hold two of
// every object.
func refuseInsideLake(data, out string) error {
	d, err := filepath.Abs(data)
	if err != nil {
		return err
	}
	o, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	if rel, err := filepath.Rel(d, o); err == nil && (rel == "." || !strings.HasPrefix(rel, "..")) {
		return fmt.Errorf("--out %s is inside the lake %s; back up to another directory", out, data)
	}
	return nil
}

// backupCatalog writes a VACUUM INTO copy next to dest, syncs it, and
// renames it over dest, so a second backup replaces the first and a
// failed one leaves the last good copy. It returns the copy's session
// count.
func backupCatalog(src, dest string) (int, error) {
	cat, err := catalog.OpenReadOnly(src)
	if err != nil {
		return 0, err
	}
	defer cat.Close()
	tmp := dest + ".tmp"
	if err := os.Remove(tmp); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	if err := cat.VacuumInto(context.Background(), tmp); err != nil {
		return 0, err
	}
	defer os.Remove(tmp)
	if err := os.Chmod(tmp, 0o600); err != nil {
		return 0, err
	}
	if err := syncPath(tmp); err != nil {
		return 0, err
	}
	check, err := catalog.OpenReadOnly(tmp)
	if err != nil {
		return 0, err
	}
	n, err := check.Counts(context.Background())
	check.Close()
	if err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return 0, err
	}
	return n.Sessions, nil
}

func syncPath(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// copyTokens copies the token file, or each regular file of a token
// directory, at mode 0600.
func copyTokens(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copyFile(src, dst)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if err := copyFile(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func runServeFsck(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), fsckUsage)
		return nil
	}
	var data string
	var repair bool
	rest, err := parseFlags(env, args, fsckUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.BoolVar(&repair, "repair", false, "remove bad objects so an upload restores them")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), fsckUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	root := filepath.Join(data, "cas")
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("fsck: %w", err)
	}
	if repair {
		lock, err := lakelock.Acquire(data)
		if err != nil {
			return fmt.Errorf("fsck --repair: %w; stop serve first", err)
		}
		defer lock.Release()
	}
	store := &cas.Store{Root: root}
	var bad []cas.Problem
	checked, err := store.Verify(func(p cas.Problem) {
		bad = append(bad, p)
		fmt.Fprintf(env.stdout(), "bad %s\n", p)
	})
	if err != nil {
		return err
	}
	removed := 0
	if repair {
		for _, p := range bad {
			fixed, err := store.Repair(p)
			if err != nil {
				return err
			}
			if fixed {
				removed++
				fmt.Fprintf(env.stdout(), "removed %s\n", p.Digest)
			}
		}
	}
	fmt.Fprintf(env.stdout(), "checked %d entries, %d bad\n", checked, len(bad))
	idErr := fsckIdentity(env, data)
	if len(bad) == 0 {
		return idErr
	}
	if repair {
		return fmt.Errorf("fsck: %d bad entries, %d removed", len(bad), removed)
	}
	return fmt.Errorf("fsck: %d bad entries; --repair removes them", len(bad))
}

// recordedLakeID reads the lake id a catalog file has recorded, without
// writing to it. A missing catalog has recorded nothing.
func recordedLakeID(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	cat, err := catalog.OpenReadOnly(path)
	if err != nil {
		return "", err
	}
	defer cat.Close()
	return cat.LakeID(ctx)
}

// backupIdentity copies identity.json into the backup. The identity is
// made before its lake id is recorded, so a catalog snapshot that names
// a lake id should always have the file. When it does not, or the file
// names another lake, the backup fails: a restore of it would refuse to
// start.
func backupIdentity(env Env, data, out string) error {
	recorded, err := recordedLakeID(context.Background(), filepath.Join(out, "catalog.db"))
	if err != nil {
		return err
	}
	id, err := identity.Load(data)
	switch {
	case errors.Is(err, os.ErrNotExist) && recorded == "":
		return nil
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("backup: the catalog is lake %s but %s is missing; the backup would not start. Restore identity.json from an earlier backup first", recorded, identity.Path(data))
	case err != nil:
		return fmt.Errorf("backup: %w", err)
	case recorded != "" && recorded != id.LakeID:
		return fmt.Errorf("backup: the catalog is lake %s but %s holds lake %s", recorded, identity.Path(data), id.LakeID)
	}
	if err := copyFile(identity.Path(data), identity.Path(out)); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "identity: %s\n", identity.Path(out))
	return nil
}

// fsckIdentity loads identity.json and checks it against the lake id the
// catalog recorded. A missing file on a catalog that recorded nothing is
// reported and is not an error: serve makes one there. A missing or
// different file on a catalog that recorded a lake id is an error,
// because serve refuses to start on it. --repair never touches it.
func fsckIdentity(env Env, data string) error {
	recorded, err := recordedLakeID(context.Background(), filepath.Join(data, "catalog.db"))
	if err != nil {
		return err
	}
	id, err := identity.Load(data)
	switch {
	case errors.Is(err, os.ErrNotExist) && recorded == "":
		fmt.Fprintln(env.stdout(), "identity: none")
		return nil
	case errors.Is(err, os.ErrNotExist):
		fmt.Fprintf(env.stdout(), "bad identity: missing; the catalog is lake %s\n", recorded)
		return fmt.Errorf("fsck: identity.json is missing; restore it from a backup, or serve will not start")
	case err != nil:
		fmt.Fprintf(env.stdout(), "bad identity: %v\n", err)
		return fmt.Errorf("fsck: identity.json does not load; restore it from a backup")
	case recorded != "" && recorded != id.LakeID:
		fmt.Fprintf(env.stdout(), "bad identity: holds lake %s; the catalog is lake %s\n", id.LakeID, recorded)
		return fmt.Errorf("fsck: identity.json is another lake's; restore the matching one from a backup")
	}
	fmt.Fprintf(env.stdout(), "identity: %s, %d keys\n", id.LakeID, len(id.Keys))
	return nil
}
