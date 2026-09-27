package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/identity"
)

const identityUsage = `terva-lampi serve identity — print the lake id and key fingerprints

usage:
  terva-lampi serve identity [--data DIR]
  terva-lampi serve identity set-url URL [--data DIR]
  terva-lampi serve identity rotate [--overlap 336h] [--data DIR]
  terva-lampi serve identity retire KEY-ID [--compromised] [--data DIR]

Reads identity.json in the lake directory and prints the lake id, then
one line per key: its id, status, fingerprint, and when it was made.
An agent shows the same fingerprint before it registers. Compare them,
as you would an SSH host key. It runs while serve runs.

set-url records the URL agents reach this lake at, the one serve
register puts in each code. It is kept in the catalog in the lake
directory, not in any repository. http is refused unless the host is
loopback. identity prints it as public_url.

rotate adds a new signing key, endorsed by the current one, and ends
the other active keys' windows after --overlap (14 days by default).
Both keys sign during the overlap. An agent follows the endorsement
from the key it pinned at registration and moves its pin the next time
it reads the key list, at start and hourly, without registering again.

retire ends a key's window now. The last active key cannot be retired;
rotate first. --compromised also marks the key as possibly leaked: an
agent pinned to it does not follow anything it endorsed and stops
uploading, saying the lake must be registered again with register
--replace. Agents that already moved to a newer key are not affected.
See docs/policy.md for what to do after a compromise.

rotate and retire write identity.json and append to audit.jsonl.
Send serve SIGHUP, or restart it, to publish the change, and back up
the lake afterwards.

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
	if len(args) > 0 && (args[0] == "rotate" || args[0] == "retire") {
		return runServeIdentityKeys(env, args[0], args[1:])
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
		if k.EndorsedBy != "" {
			line += " endorsed_by " + k.EndorsedBy
		}
		if k.Compromised {
			line += " compromised"
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

// defaultOverlap is how long the old key keeps signing after a rotate.
// Agents read the key list hourly while they run; the rest is for
// machines that are off.
const defaultOverlap = 14 * 24 * time.Hour

func runServeIdentityKeys(env Env, sub string, args []string) error {
	var keyID string
	if sub == "retire" {
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			fmt.Fprint(env.stdout(), identityUsage)
			return errors.New("serve identity retire needs a key id; serve identity lists them")
		}
		keyID, args = args[0], args[1:]
	}
	var data string
	var compromised bool
	overlap := defaultOverlap
	rest, err := parseFlags(env, args, identityUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		if sub == "rotate" {
			fs.DurationVar(&overlap, "overlap", overlap, "how long the old key keeps signing")
		} else {
			fs.BoolVar(&compromised, "compromised", false, "the key may have leaked")
		}
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), identityUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if overlap < 0 {
		return errors.New("--overlap is negative")
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	// Update holds identity.lock from the load to the save, so a rotate
	// and a retire that run at once cannot undo each other.
	now := time.Now()
	var k identity.Key
	err = identity.Update(data, func(id *identity.Identity) error {
		if sub == "rotate" {
			var err error
			k, err = id.Rotate(rand.Reader, now, overlap)
			return err
		}
		return id.Retire(keyID, now, compromised)
	})
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no identity yet; serve makes one on its next start", data)
	}
	if err != nil {
		return err
	}
	var event audit.Event
	switch sub {
	case "rotate":
		fmt.Fprintf(env.stdout(), "added key %s %s endorsed_by %s; the other keys stop signing at %s\n", k.ID, identity.Fingerprint(k.Pub), k.EndorsedBy, now.Add(overlap).UTC().Format(time.RFC3339))
		event = audit.Event{Time: now, Kind: audit.KeyAdded, Actor: "serve identity rotate", Detail: "key=" + k.ID + " endorsed_by=" + k.EndorsedBy}
	case "retire":
		detail := "key=" + keyID
		if compromised {
			detail += " compromised"
			fmt.Fprintf(env.stdout(), "retired key %s as compromised; agents pinned only to it stop and must run register --replace with a new code\n", keyID)
		} else {
			fmt.Fprintf(env.stdout(), "retired key %s\n", keyID)
		}
		event = audit.Event{Time: now, Kind: audit.KeyRetired, Actor: "serve identity retire", Detail: detail}
	}
	fmt.Fprintln(env.stdout(), "send serve SIGHUP (systemctl reload) or restart it to publish the change, then back up the lake")
	if err := audit.Append(data, event); err != nil {
		return fmt.Errorf("the change is saved, but writing it to %s failed: %w", audit.FileName, err)
	}
	return nil
}
