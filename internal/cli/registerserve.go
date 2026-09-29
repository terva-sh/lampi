package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/upload"
)

const serveRegisterUsage = `terva-lampi serve register — mint, list, or revoke registration codes

usage:
  terva-lampi serve register --name NAME [--expires 24h] [--profile P] [--bay BAY]... [--data DIR]
  terva-lampi serve register --list [--data DIR]
  terva-lampi serve register --revoke NAME|ID [--data DIR]

A registration code lets one new machine become a device of this lake,
once, before it expires. The code holds the lake's public URL, its lake
id, the key that signed the code, a one-time secret and the expiry. It
does not hold a device token: the agent makes its own and sends only
its hash. The lake stores only the hash of the secret.

Minting needs the public URL, set once with serve identity set-url.
Before it prints a code, serve register fetches the key list through
that URL and refuses when it is not this lake, which catches a wrong
URL or a proxy that does not forward /.well-known/terva-lampi/.

The code goes to stdout and nothing else does. It is a secret: hand it
over on stdin or in a file, not as a command argument, and not where it
is logged. --name is the device's name, --expires its lifetime (at most
30 days), --profile the profile its agent fetches (see serve --help).
--bay, repeated, names a bay the device may upload into, by id, name or
alias (see serve bays). Without --bay the device writes the default bay,
the inbox.

--list prints each code: its id, device name, state (pending, used,
expired or revoked), profile, bays and expiry. --revoke stops a pending code;
a used code made a device, which serve devices revoke stops.

Every mint, redemption, expiry, revocation and refused attempt goes to
audit.jsonl in the lake directory. An expiry is written once, the first
time serve register runs or the code is presented after it expired.
Codes live in the catalog, so a backup keeps them.
`

func runServeRegister(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), serveRegisterUsage)
		return nil
	}
	var data, name, profile, revoke string
	var list bool
	var bays bayList
	expires := 24 * time.Hour
	rest, err := parseFlags(env, args, serveRegisterUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&name, "name", "", "device name")
		fs.StringVar(&profile, "profile", "", "profile the device's agent fetches (default: default)")
		fs.DurationVar(&expires, "expires", expires, "how long the code stays valid")
		fs.BoolVar(&list, "list", false, "list codes")
		fs.StringVar(&revoke, "revoke", "", "revoke a pending code by device name or id")
		fs.Var(&bays, "bay", "a bay the device may write (repeatable)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), serveRegisterUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	modes := 0
	for _, on := range []bool{name != "", list, revoke != ""} {
		if on {
			modes++
		}
	}
	if modes != 1 {
		fmt.Fprint(env.stdout(), serveRegisterUsage)
		return errors.New("serve register needs exactly one of --name, --list or --revoke")
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve register: %w", err)
	}
	ctx := context.Background()
	now := time.Now()
	cat, err := catalog.OpenCurrent(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	lake := registrar.Lake{Catalog: cat, Dir: data}
	// The lake is the authority on expiry, and nothing runs in the
	// background to notice it, so every serve register records the codes
	// that expired since the last look; List does that too.
	if list {
		regs, err := registrar.List(ctx, lake, "serve register", now)
		if errors.Is(err, registrar.ErrAuditQueued) {
			fmt.Fprintf(env.stderr(), "terva-lampi serve register: %v\n", err)
		} else if err != nil {
			return err
		}
		for _, r := range regs {
			prof := r.Profile
			if prof == "" {
				prof = config.DefaultProfile
			}
			codeBays := catalog.DefaultBayName
			if len(r.Bays) > 0 {
				codeBays = strings.Join(r.Bays, ",")
			}
			line := fmt.Sprintf("%s %s %s profile=%s bays=%s expires=%s", r.ID, r.Name, r.State(now), prof, codeBays, r.Expires.Format(time.RFC3339))
			if r.DeviceID != "" {
				line += " device=" + r.DeviceID
			}
			if r.CreatedBy != "" {
				line += " created_by=" + listValue(r.CreatedBy)
			}
			if r.RevokedBy != "" {
				line += " revoked_by=" + listValue(r.RevokedBy)
			}
			fmt.Fprintln(env.stdout(), line)
		}
		if len(regs) == 0 {
			fmt.Fprintln(env.stdout(), "no registration codes")
		}
		return nil
	}
	// Queued lines that cannot be written yet do not stop the command:
	// they stay queued, and a mint still refuses to show a code whose
	// own line is not written.
	if err := registrar.AuditExpiries(ctx, lake, "serve register", now); errors.Is(err, registrar.ErrAuditQueued) {
		fmt.Fprintf(env.stderr(), "terva-lampi serve register: %v\n", err)
	} else if err != nil {
		return err
	}
	if revoke != "" {
		r, err := registrar.Revoke(ctx, lake, revoke, registrar.Actor{Catalog: catalog.ActorCLI, Audit: "serve register --revoke"}, now)
		if errors.Is(err, catalog.ErrNoRegistration) {
			return fmt.Errorf("no pending code for %s; serve register --list shows them", revoke)
		}
		if errors.Is(err, catalog.ErrRegistrationRevoked) {
			// Nothing changed, so there is nothing to audit.
			fmt.Fprintf(env.stdout(), "%s (%s) was already revoked\n", r.ID, r.Name)
			return nil
		}
		if r.ID != "" {
			fmt.Fprintf(env.stdout(), "revoked %s (%s)\n", r.ID, r.Name)
		}
		return err
	}

	if expires <= 0 || expires > registrar.MaxLifetime {
		return fmt.Errorf("--expires %s: a code lives more than 0 and at most %s", expires, registrar.MaxLifetime)
	}
	known, err := cat.HasProfile(ctx, profile)
	if err != nil {
		return err
	}
	if !known {
		return fmt.Errorf("no profile named %s in the lake's catalog", profile)
	}
	lake.Identity, err = identity.Load(data)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no identity yet; serve makes one on its next start", data)
	}
	if err != nil {
		return err
	}
	// The lake host is admin access: any bay.
	m, err := registrar.Mint(ctx, lake, name, profile, bays, expires, registrar.Actor{Catalog: catalog.ActorCLI, Audit: "serve register", Minter: catalog.Minter{Admin: true}}, now)
	if err != nil {
		return err
	}
	fmt.Fprintln(env.stdout(), m.Code)
	fmt.Fprintf(env.stderr(), "terva-lampi serve register: code %s for %s expires %s; lake %s key fingerprint %s\n",
		m.Registration.ID, m.Registration.Name, m.Registration.Expires.Format(time.RFC3339), m.LakeID, m.Fingerprint)
	return nil
}

// runServeIdentitySetURL records the lake's public URL.
func runServeIdentitySetURL(env Env, args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprint(env.stdout(), identityUsage)
		return errors.New("serve identity set-url needs one URL")
	}
	given := args[0]
	var data string
	rest, err := parseFlags(env, args[1:], identityUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), identityUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	raw := strings.TrimRight(given, "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("%q is not a base URL: use https://host[:port][/path]", given)
	}
	// Agents will send their token to this URL. Plain http is refused
	// unless it is loopback, the rule every client applies.
	if err := upload.CheckToken(raw, "x"); err != nil {
		return fmt.Errorf("%s: agents would send their token over plain http; use https", raw)
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve identity set-url: %w", err)
	}
	cat, err := catalog.OpenCurrent(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	if err := cat.SetPublicURL(context.Background(), raw); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "public_url %s\n", raw)
	return nil
}

// listValue quotes a value that would not read as one field, such as a
// dashboard operator's display name.
func listValue(v string) string {
	if v == "" || strings.ContainsFunc(v, func(r rune) bool { return r == '"' || r == '\\' || unicode.IsSpace(r) || !unicode.IsPrint(r) }) {
		return strconv.Quote(v)
	}
	return v
}

// bayList is a repeatable --bay flag.
type bayList []string

func (b *bayList) String() string { return strings.Join(*b, ",") }

func (b *bayList) Set(v string) error {
	if v == "" {
		return errors.New("empty bay")
	}
	*b = append(*b, v)
	return nil
}
