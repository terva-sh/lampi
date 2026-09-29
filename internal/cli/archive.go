package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"

	"terva.sh/lampi/internal/archive"
	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/identity"
)

// archiveEntryHook runs before each archive entry is added. A test sets
// it to fail part-way; it is nil otherwise.
var archiveEntryHook func(name string) error

// recipientList collects --recipient flags.
type recipientList []string

func (r *recipientList) String() string     { return strings.Join(*r, ",") }
func (r *recipientList) Set(v string) error { *r = append(*r, v); return nil }

// loadRecipients parses the --recipient values and the recipients
// file, if any. Recipients are public keys, so a flag is a fine place
// for one; an identity never is.
func loadRecipients(flags []string, file string) ([]age.Recipient, error) {
	var out []age.Recipient
	if len(flags) > 0 {
		rs, err := archive.ReadRecipients(strings.NewReader(strings.Join(flags, "\n")))
		if err != nil {
			return nil, fmt.Errorf("--recipient: %w", err)
		}
		out = append(out, rs...)
	}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		rs, err := archive.ReadRecipients(f)
		if err != nil {
			return nil, fmt.Errorf("--recipients-file %s: %w", file, err)
		}
		out = append(out, rs...)
	}
	if len(out) == 0 {
		return nil, errors.New("serve backup --archive needs --recipient or --recipients-file")
	}
	return out, nil
}

// backupArchive writes the lake to one encrypted archive at dest. The
// archive is built in a temp file beside dest and renamed over it only
// once the age payload is closed and the file synced; a failure or a
// signal removes the temp file and leaves dest as it was. The catalog
// snapshot is the one plaintext temp file, kept in the lake directory,
// which holds that plaintext already, and removed at the end.
func backupArchive(env Env, data, dest, tokenFile string, recipients []age.Recipient) (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	snap, err := os.CreateTemp(data, ".backup-catalog-*.db")
	if err != nil {
		return err
	}
	snapName := snap.Name()
	snap.Close()
	defer os.Remove(snapName)
	// VACUUM INTO refuses a path that exists.
	if err := os.Remove(snapName); err != nil {
		return err
	}
	sessions, err := snapshotCatalog(filepath.Join(data, "catalog.db"), snapName)
	if err != nil {
		return err
	}
	hasID, lakeID, err := checkBackupIdentity(snapName, data)
	if err != nil {
		return err
	}

	dir := filepath.Dir(dest)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}

	w, err := archive.NewWriter(tmp, recipients, archive.Manifest{
		LakeID:        lakeID,
		Created:       time.Now().UTC().Format(time.RFC3339),
		LampiVersion:  version,
		CatalogSchema: catalog.SchemaVersion(),
	})
	if err != nil {
		return err
	}
	entries := 1
	add := func(name string, size int64, r io.Reader) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("backup interrupted: %w", err)
		}
		if archiveEntryHook != nil {
			if err := archiveEntryHook(name); err != nil {
				return err
			}
		}
		entries++
		return w.Add(name, size, r)
	}
	addFile := func(name, path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		return add(name, st.Size(), f)
	}

	if err := addFile("catalog.db", snapName); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "catalog.db: %d sessions\n", sessions)
	store := &cas.Store{Root: filepath.Join(data, "cas")}
	n, err := store.Export(func(rel string, size int64, r io.Reader) error {
		return add("cas/"+rel, size, r)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "cas: %d entries\n", n)
	if hasID {
		if err := addFile(identity.FileName, identity.Path(data)); err != nil {
			return err
		}
		fmt.Fprintln(env.stdout(), "identity: included")
	}
	if _, err := os.Stat(audit.Path(data)); err == nil {
		if err := addFile(audit.FileName, audit.Path(data)); err != nil {
			return err
		}
		fmt.Fprintln(env.stdout(), "audit: included")
	}
	if tokenFile != "" {
		if err := addTokens(tokenFile, addFile); err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "token file: included as %s\n", filepath.Base(filepath.Clean(tokenFile)))
	}

	if err := w.Close(); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	st, err := tmp.Stat()
	if err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("backup interrupted: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	tmpName = ""
	if err := syncDirPath(dir); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "archive: %s, %d entries, %.1f MiB, %d recipients\n", dest, entries, float64(st.Size())/(1<<20), len(recipients))
	return nil
}

// addTokens adds the token file, or each regular file of a token
// directory, under the base name the directory backup copies it to.
func addTokens(src string, addFile func(name, path string) error) error {
	base := filepath.Base(filepath.Clean(src))
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return addFile(base, src)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if err := addFile(base+"/"+e.Name(), filepath.Join(src, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// snapshotCatalog writes a VACUUM INTO copy of src at dest, owner-only,
// and returns its session count.
func snapshotCatalog(src, dest string) (int, error) {
	cat, err := catalog.OpenReadOnly(src)
	if err != nil {
		return 0, err
	}
	defer cat.Close()
	if err := cat.VacuumInto(context.Background(), dest); err != nil {
		return 0, err
	}
	if err := os.Chmod(dest, 0o600); err != nil {
		return 0, err
	}
	check, err := catalog.OpenReadOnly(dest)
	if err != nil {
		return 0, err
	}
	defer check.Close()
	n, err := check.Counts(context.Background())
	if err != nil {
		return 0, err
	}
	return n.Sessions, nil
}

func syncDirPath(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

const restoreUsage = `terva-lampi serve restore — restore an encrypted backup archive

usage:
  terva-lampi serve restore --archive FILE --identity-file PATH --data DIR

Decrypts FILE, an archive serve backup --archive wrote, with the age
identity in PATH, and writes the lake it holds into DIR: the catalog,
the CAS, identity.json, audit.jsonl and the token file, under the name
it had. DIR must not exist or must be empty. Files are written 0600 and
directories 0700.

The identity is read from a file only, never a flag, so it stays out of
shell history and the process list. Keep it off the lake's host.

A wrong identity, an archive that fails authentication or is cut short,
an entry that would leave DIR, or an archive format this version does
not know stops the restore, and what it wrote is removed: DIR itself
when the restore created it.

After extracting, restore checks the result as serve fsck does: every
object is re-hashed, identity.json is checked against the catalog, and
the catalog is opened. A problem found there is reported and the
restored files are kept, for serve fsck --repair.

Start serve on DIR with --token-file pointing at the restored token
file. normalized/, parquet/ and search.db are rebuilt from the record.
`

func runServeRestore(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), restoreUsage)
		return nil
	}
	var file, identityFile, data string
	rest, err := parseFlags(env, args, restoreUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&file, "archive", "", "encrypted backup archive")
		fs.StringVar(&identityFile, "identity-file", "", "age identity file")
		fs.StringVar(&data, "data", "", "lake directory to restore into; must be new or empty")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), restoreUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if file == "" || identityFile == "" || data == "" {
		fmt.Fprint(env.stdout(), restoreUsage)
		return errors.New("serve restore needs --archive, --identity-file and --data")
	}
	idf, err := os.Open(identityFile)
	if err != nil {
		return err
	}
	ids, err := archive.ReadIdentities(idf)
	idf.Close()
	if err != nil {
		return fmt.Errorf("--identity-file %s: %w", identityFile, err)
	}

	created := false
	if entries, err := os.ReadDir(data); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(data, 0o700); err != nil {
			return err
		}
		created = true
	} else if err != nil {
		return err
	} else if len(entries) > 0 {
		return fmt.Errorf("serve restore: %s is not empty; restore into a new directory", data)
	}

	in, err := os.Open(file)
	if err != nil {
		cleanRestore(data, created)
		return err
	}
	m, files, err := archive.Extract(in, ids, data)
	in.Close()
	if err != nil {
		cleanRestore(data, created)
		return fmt.Errorf("serve restore: %w", err)
	}
	fmt.Fprintf(env.stdout(), "restored %d files from lake %s, taken %s by terva-lampi %s (catalog schema %d)\n", files, orNone(m.LakeID), m.Created, m.LampiVersion, m.CatalogSchema)
	// The manifest is the archive's, not the lake's.
	if err := os.Remove(filepath.Join(data, archive.ManifestName)); err != nil {
		return err
	}
	return checkRestored(env, data)
}

// checkRestored opens the catalog and runs fsck's checks on a restored
// lake directory.
func checkRestored(env Env, data string) error {
	cat, err := catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
	if err != nil {
		return fmt.Errorf("serve restore: the restored catalog does not open: %w", err)
	}
	n, err := cat.Counts(context.Background())
	cat.Close()
	if err != nil {
		return fmt.Errorf("serve restore: %w", err)
	}
	fmt.Fprintf(env.stdout(), "catalog.db: %d sessions\n", n.Sessions)
	if err := runServeFsck(env, []string{"--data", data}); err != nil {
		return fmt.Errorf("serve restore: the restored lake is in %s but fsck failed: %w", data, err)
	}
	return nil
}

// cleanRestore removes what a failed restore wrote: data itself when
// the restore created it, else everything inside it, which was empty.
func cleanRestore(data string, created bool) {
	if created {
		os.RemoveAll(data)
		return
	}
	entries, _ := os.ReadDir(data)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(data, e.Name()))
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
