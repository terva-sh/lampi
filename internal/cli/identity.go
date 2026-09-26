package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/identity"
)

const identityUsage = `terva-lampi serve identity — print the lake id and key fingerprints

usage:
  terva-lampi serve identity [--data DIR]
  terva-lampi serve identity set-url URL [--data DIR]

Reads identity.json in the lake directory and prints the lake id, then
one line per key: its id, status, fingerprint, and when it was made.
An agent shows the same fingerprint before it registers. Compare them,
as you would an SSH host key. It runs while serve runs.

set-url records the URL agents reach this lake at, the one serve
register puts in each code. It is kept in the catalog in the lake
directory, not in any repository. http is refused unless the host is
loopback. identity prints it as public_url.

serve makes the identity on its first start. A lake that has not
started since the upgrade has none yet.
`

func runServeIdentity(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), identityUsage)
		return nil
	}
	if len(args) > 0 && args[0] == "set-url" {
		return runServeIdentitySetURL(env, args[1:])
	}
	var data string
	rest, err := parseFlags(env, args, identityUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), identityUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	id, err := identity.Load(data)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no identity yet; serve makes one on its next start", data)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "lake_id %s\n", id.LakeID)
	if public := recordedPublicURL(data); public != "" {
		fmt.Fprintf(env.stdout(), "public_url %s\n", public)
	}
	for _, k := range id.Keys {
		line := fmt.Sprintf("key %s %s %s created %s", k.ID, k.Status, identity.Fingerprint(k.Pub), k.Created.UTC().Format("2006-01-02T15:04:05Z"))
		if !k.NotAfter.IsZero() {
			line += " not_after " + k.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
		}
		fmt.Fprintln(env.stdout(), line)
	}
	return nil
}

// recordedPublicURL is the public URL in the catalog, or "" when none
// is set or the catalog cannot be read.
func recordedPublicURL(data string) string {
	cat, err := catalog.OpenReadOnly(filepath.Join(data, "catalog.db"))
	if err != nil {
		return ""
	}
	defer cat.Close()
	u, _ := cat.PublicURL(context.Background())
	return u
}
