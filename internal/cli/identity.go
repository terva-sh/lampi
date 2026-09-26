package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"terva.sh/lampi/internal/identity"
)

const identityUsage = `terva-lampi serve identity — print the lake id and key fingerprints

usage:
  terva-lampi serve identity [--data DIR]

Reads identity.json in the lake directory and prints the lake id, then
one line per key: its id, status, fingerprint, and when it was made.
An agent shows the same fingerprint before it registers. Compare them,
as you would an SSH host key. It runs while serve runs.

serve makes the identity on its first start. A lake that has not
started since the upgrade has none yet.
`

func runServeIdentity(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), identityUsage)
		return nil
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
	for _, k := range id.Keys {
		line := fmt.Sprintf("key %s %s %s created %s", k.ID, k.Status, identity.Fingerprint(k.Pub), k.Created.UTC().Format("2006-01-02T15:04:05Z"))
		if !k.NotAfter.IsZero() {
			line += " not_after " + k.NotAfter.UTC().Format("2006-01-02T15:04:05Z")
		}
		fmt.Fprintln(env.stdout(), line)
	}
	return nil
}
