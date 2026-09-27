package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
	"terva.sh/lampi/internal/upload"
)

const serveRegisterUsage = `terva-lampi serve register — mint, list, or revoke registration codes

usage:
  terva-lampi serve register --name NAME [--expires 24h] [--profile P] [--data DIR] [--profiles PATH]
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

--list prints each code: its id, device name, state (pending, used,
expired or revoked), profile and expiry. --revoke stops a pending code;
a used code made a device, which serve devices revoke stops.

Every mint, redemption, expiry, revocation and refused attempt goes to
audit.jsonl in the lake directory. An expiry is written once, the first
time serve register runs or the code is presented after it expired.
Codes live in the catalog, so a backup keeps them.
`

// maxCodeLifetime bounds --expires.
const maxCodeLifetime = 30 * 24 * time.Hour

func runServeRegister(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), serveRegisterUsage)
		return nil
	}
	var data, profilesFile, name, profile, revoke string
	var list bool
	expires := 24 * time.Hour
	rest, err := parseFlags(env, args, serveRegisterUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&profilesFile, "profiles", "", "agent profiles file (default: profiles.json in the lake directory)")
		fs.StringVar(&name, "name", "", "device name")
		fs.StringVar(&profile, "profile", "", "profile the device's agent fetches (default: default)")
		fs.DurationVar(&expires, "expires", expires, "how long the code stays valid")
		fs.BoolVar(&list, "list", false, "list codes")
		fs.StringVar(&revoke, "revoke", "", "revoke a pending code by device name or id")
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
	cat, err := catalog.Open(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	// The lake is the authority on expiry, and nothing runs in the
	// background to notice it, so every serve register records the codes
	// that expired since the last look.
	if err := auditExpiries(ctx, cat, data, now); err != nil {
		return err
	}
	if list {
		regs, err := cat.Registrations(ctx)
		if err != nil {
			return err
		}
		for _, r := range regs {
			prof := r.Profile
			if prof == "" {
				prof = config.DefaultProfile
			}
			line := fmt.Sprintf("%s %s %s profile=%s expires=%s", r.ID, r.Name, r.State(now), prof, r.Expires.Format(time.RFC3339))
			if r.DeviceID != "" {
				line += " device=" + r.DeviceID
			}
			if r.CreatedBy != "" {
				line += " created_by=" + r.CreatedBy
			}
			if r.RevokedBy != "" {
				line += " revoked_by=" + r.RevokedBy
			}
			fmt.Fprintln(env.stdout(), line)
		}
		if len(regs) == 0 {
			fmt.Fprintln(env.stdout(), "no registration codes")
		}
		return nil
	}
	if revoke != "" {
		r, err := cat.RevokeRegistration(ctx, revoke, catalog.ActorCLI, now)
		if errors.Is(err, catalog.ErrNoRegistration) {
			return fmt.Errorf("no pending code for %s; serve register --list shows them", revoke)
		}
		if errors.Is(err, catalog.ErrRegistrationRevoked) {
			// Nothing changed, so there is nothing to audit.
			fmt.Fprintf(env.stdout(), "%s (%s) was already revoked\n", r.ID, r.Name)
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "revoked %s (%s)\n", r.ID, r.Name)
		if err := audit.Append(data, audit.Event{Time: now, Kind: audit.RegistrationRevoked, Device: r.Name, Actor: "serve register --revoke", Detail: "registration=" + r.ID}); err != nil {
			return fmt.Errorf("revoked %s, but writing it to %s failed: %w; the change stands", r.ID, audit.FileName, err)
		}
		return nil
	}

	if expires <= 0 || expires > maxCodeLifetime {
		return fmt.Errorf("--expires %s: a code lives more than 0 and at most %s", expires, maxCodeLifetime)
	}
	if profile != "" && profile != config.DefaultProfile {
		if profilesFile == "" {
			profilesFile = filepath.Join(data, config.ProfilesFileName)
		}
		profiles, err := config.LoadProfiles(profilesFile)
		if err != nil {
			return err
		}
		if _, ok := profiles[profile]; !ok {
			return fmt.Errorf("no profile named %s in %s", profile, profilesFile)
		}
	} else {
		profile = ""
	}
	id, err := identity.Load(data)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s has no identity yet; serve makes one on its next start", data)
	}
	if err != nil {
		return err
	}
	public, err := cat.PublicURL(ctx)
	if err != nil {
		return err
	}
	if public == "" {
		return errors.New("the lake has no public URL; set it with serve identity set-url URL")
	}
	if err := checkPublicURL(ctx, public, id, now); err != nil {
		return fmt.Errorf("the public URL %s does not reach this lake: %w; check serve identity set-url, and that the proxy forwards %s", public, err, protocol.KeysPath)
	}
	secret, err := regcode.NewSecret()
	if err != nil {
		return err
	}
	cur, _ := id.Current(now)
	reg, err := cat.CreateRegistration(ctx, name, regcode.HashSecret(secret), profile, cur.ID, catalog.ActorCLI, now, now.Add(expires))
	if err != nil {
		return err
	}
	code, err := regcode.Encode(id, regcode.Code{URL: public, Secret: secret, Expires: reg.Expires}, now)
	if err != nil {
		return err
	}
	prof := profile
	if prof == "" {
		prof = config.DefaultProfile
	}
	// The mint is recorded before the code is printed. A code whose mint
	// the audit log does not hold is revoked and never shown.
	if err := audit.Append(data, audit.Event{Time: now, Kind: audit.RegistrationCreated, Device: reg.Name, Actor: "serve register",
		Detail: fmt.Sprintf("registration=%s profile=%s expires=%s", reg.ID, prof, reg.Expires.Format(time.RFC3339))}); err != nil {
		if _, rerr := cat.RevokeRegistration(ctx, reg.ID, catalog.ActorCLI, now); rerr != nil {
			return fmt.Errorf("writing the mint of %s to %s failed: %w; the code was not printed, but revoking it also failed: %v; run serve register --revoke %s", reg.ID, audit.FileName, err, rerr, reg.ID)
		}
		return fmt.Errorf("writing the mint of %s to %s failed: %w; the code was revoked and not printed; fix the audit log and mint again", reg.ID, audit.FileName, err)
	}
	fmt.Fprintln(env.stdout(), code)
	fmt.Fprintf(env.stderr(), "terva-lampi serve register: code %s for %s expires %s; lake %s key fingerprint %s\n",
		reg.ID, reg.Name, reg.Expires.Format(time.RFC3339), id.LakeID, currentFingerprint(id, now))
	return nil
}

// auditExpiries writes a registration.expired line for each code the
// catalog has not recorded as expired yet. serve writes the same line
// when an expired code is presented; the catalog hands each code to one
// of them.
func auditExpiries(ctx context.Context, cat *catalog.Catalog, data string, now time.Time) error {
	regs, err := cat.RecordExpiries(ctx, now)
	if err != nil {
		return err
	}
	// Each code is marked already, so one failed line does not stop the
	// rest from being written.
	var failed []string
	var first error
	for _, r := range regs {
		if err := audit.Append(data, audit.Event{Time: now, Kind: audit.RegistrationExpired, Device: r.Name, Actor: "serve register",
			Detail: "registration=" + r.ID + " expires=" + r.Expires.Format(time.RFC3339)}); err != nil {
			failed = append(failed, r.ID)
			if first == nil {
				first = err
			}
		}
	}
	if first != nil {
		return fmt.Errorf("writing the expiry of %s to %s failed: %w; the codes are expired either way, but these lines will not be written again; fix the audit log", strings.Join(failed, ", "), audit.FileName, first)
	}
	return nil
}

// checkPublicURL fetches the key list through public and checks that it
// is this lake's, signed over a fresh nonce.
func checkPublicURL(ctx context.Context, public string, id *identity.Identity, now time.Time) error {
	cur, ok := id.Current(now)
	if !ok {
		return errors.New("this lake has no active key")
	}
	pub := id.Public()
	for _, k := range pub {
		if k.ID == cur.ID {
			return verifyKeyList(ctx, public, id.LakeID, k)
		}
	}
	return errors.New("this lake's active key is not in its key list")
}

// verifyKeyList fetches the key list at server over a fresh nonce and
// checks that it is lake lakeID's, that key is listed there as active,
// and that key signed it over the nonce. register runs the same check
// against the key a code names.
func verifyKeyList(ctx context.Context, server, lakeID string, key protocol.LakeKey) error {
	pub, err := identity.ParsePublic(key)
	if err != nil {
		return err
	}
	nonce, err := identity.NewNonce()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	signed, p, err := upload.FetchKeys(ctx, server, nonce)
	if err != nil {
		return err
	}
	// The key's place in the list first, for a precise reason. A list
	// that lies about it can only make this refuse.
	listed := false
	for _, k := range p.Keys {
		if k.ID == key.ID && k.PublicKey == key.PublicKey {
			listed = true
			if k.Compromised {
				return fmt.Errorf("key %s is marked compromised there", key.ID)
			}
			if k.Status != identity.StatusActive {
				return fmt.Errorf("key %s is %s there, not active", key.ID, k.Status)
			}
		}
	}
	if !listed {
		return fmt.Errorf("key %s is not in the key list there", key.ID)
	}
	if err := identity.Verify(identity.ContextKeys, signed, pub); err != nil {
		return fmt.Errorf("the key list there is not signed by key %s: %w", key.ID, err)
	}
	if p.Nonce != nonce {
		return errors.New("the key list there does not carry the nonce sent: it is a replay or a cache")
	}
	if p.LakeID != lakeID {
		return fmt.Errorf("the key list there is for lake %s, not %s", p.LakeID, lakeID)
	}
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
	cat, err := catalog.Open(path)
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

func currentFingerprint(id *identity.Identity, now time.Time) string {
	k, ok := id.Current(now)
	if !ok {
		return "(no active key)"
	}
	return identity.Fingerprint(k.Pub)
}
