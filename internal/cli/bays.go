package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
)

const baysUsage = `terva-lampi serve bays — manage the lake's bays

usage:
  terva-lampi serve bays [list] [--data DIR]
  terva-lampi serve bays create NAME [--data DIR]
  terva-lampi serve bays rename BAY NEW-NAME [--data DIR]
  terva-lampi serve bays alias BAY ALIAS [--data DIR]
  terva-lampi serve bays unalias ALIAS [--data DIR]
  terva-lampi serve bays delete BAY --yes [--data DIR]
  terva-lampi serve bays default on|off [--data DIR]
  terva-lampi serve bays grants [--data DIR]
  terva-lampi serve bays grant BAY (--group G | --device NAME | --read-token ID) (--read | --write) [--data DIR]
  terva-lampi serve bays revoke BAY (--group G | --device NAME | --read-token ID) (--read | --write) [--data DIR]

A bay is a named segment of the lake and an access boundary
(docs/policy.md#bays). A session is in one or more bays. BAY is a bay's
id, name or alias. A name is 1 to 63 lowercase letters, digits and
dashes.

list prints each bay with its id, aliases, session count, and whether
it is the default. The default bay is the inbox: sessions nothing places
land there, and only admins and principals granted it read it.

rename keeps the old name as an alias, so agents that request the bay
by it still reach it. The default bay keeps its name; alias it instead.

delete takes the bay out of every session. A session left in no other
bay moves to the default bay. No data is deleted.

default off refuses, at ingest, a new session nothing places, and the
agent keeps it. The default bay stays, with what is in it.

grant and revoke change who reads or writes a bay. A group is an IdP
group from the web config's groups claim: read lets its viewers and
operators read the bay, and write lets its operators mint codes into
it. A device with write uploads into the bay. A read token with read
reads the bay's raw artifacts. Admins read every bay without a grant.

Every command runs while serve runs. The changes are written to the
catalog and to audit.jsonl in the lake directory.
`

func runServeBays(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), baysUsage)
		return nil
	}
	sub := "list"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		sub, args = args[0], args[1:]
	}
	need := map[string]int{"list": 0, "grants": 0, "create": 1, "unalias": 1, "delete": 1, "default": 1, "grant": 1, "revoke": 1, "rename": 2, "alias": 2}
	n, known := need[sub]
	if !known {
		fmt.Fprint(env.stdout(), baysUsage)
		return fmt.Errorf("unknown serve bays command %q", sub)
	}
	var pos []string
	for len(pos) < n {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			fmt.Fprint(env.stdout(), baysUsage)
			return fmt.Errorf("serve bays %s needs %d argument(s)", sub, n)
		}
		pos, args = append(pos, args[0]), args[1:]
	}
	var data, group, device, token string
	var read, write, yes bool
	rest, err := parseFlags(env, args, baysUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&group, "group", "", "an IdP group")
		fs.StringVar(&device, "device", "", "a device name")
		fs.StringVar(&token, "read-token", "", "a read token id (rtk_…)")
		fs.BoolVar(&read, "read", false, "the read permission")
		fs.BoolVar(&write, "write", false, "the write permission")
		fs.BoolVar(&yes, "yes", false, "confirm delete")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), baysUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve bays: %w", err)
	}
	ctx := context.Background()
	if sub == "list" || sub == "grants" {
		cat, err := catalog.OpenReadOnly(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		if sub == "list" {
			return listBays(ctx, env, cat)
		}
		return listGrants(ctx, env, cat)
	}
	cat, err := catalog.OpenCurrent(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	now := time.Now()
	actor := "serve bays " + sub
	var done string
	switch sub {
	case "create":
		b, err := cat.CreateBay(ctx, pos[0], actor, now)
		if err != nil {
			return err
		}
		done = fmt.Sprintf("created bay %s (%s)", b.Name, b.ID)
	case "rename":
		if err := cat.RenameBay(ctx, pos[0], pos[1], actor, now); err != nil {
			return err
		}
		done = fmt.Sprintf("renamed %s to %s; %s stays as an alias", pos[0], pos[1], pos[0])
	case "alias":
		if err := cat.AliasBay(ctx, pos[0], pos[1], actor, now); err != nil {
			return err
		}
		done = fmt.Sprintf("%s is now an alias of %s", pos[1], pos[0])
	case "unalias":
		if err := cat.UnaliasBay(ctx, pos[0], actor, now); err != nil {
			return err
		}
		done = "removed alias " + pos[0]
	case "delete":
		if !yes {
			return fmt.Errorf("serve bays delete %s takes it out of every session; pass --yes to delete it", pos[0])
		}
		moved, err := cat.DeleteBay(ctx, pos[0], actor, now)
		if err != nil {
			return err
		}
		done = fmt.Sprintf("deleted bay %s; %d sessions in no other bay moved to the default", pos[0], moved)
	case "default":
		if pos[0] != "on" && pos[0] != "off" {
			return fmt.Errorf("serve bays default takes on or off, not %q", pos[0])
		}
		if err := cat.SetDefaultEnabled(ctx, pos[0] == "on", actor, now); err != nil {
			return err
		}
		done = "default bay turned " + pos[0]
	case "grant", "revoke":
		kind, principal, perm, err := grantArgs(ctx, cat, group, device, token, read, write)
		if err != nil {
			fmt.Fprint(env.stdout(), baysUsage)
			return err
		}
		change := cat.AddGrant
		if sub == "revoke" {
			change = cat.RemoveGrant
		}
		changed, err := change(ctx, kind, principal, pos[0], perm, actor, now)
		if err != nil {
			return err
		}
		switch {
		case !changed && sub == "grant":
			fmt.Fprintf(env.stdout(), "%s %s already holds %s on %s\n", kind, principal, perm, pos[0])
			return nil
		case !changed:
			fmt.Fprintf(env.stdout(), "%s %s holds no %s on %s\n", kind, principal, perm, pos[0])
			return nil
		case sub == "grant":
			done = fmt.Sprintf("granted %s %s %s on %s", kind, principal, perm, pos[0])
		default:
			done = fmt.Sprintf("revoked %s %s %s on %s", kind, principal, perm, pos[0])
		}
	}
	// The change is committed. Say so first, so a failed audit write is
	// not read as a change that did not happen.
	fmt.Fprintln(env.stdout(), done)
	if err := cat.FlushAudit(ctx, data); err != nil {
		return fmt.Errorf("%s, but writing it to %s failed: %w; the change stands, and the line stays queued until the audit log can be written", done, audit.FileName, err)
	}
	return nil
}

// grantArgs reads exactly one principal and one permission from the
// flags. A device is named by name and stored by id, so a rename of the
// device does not orphan its grants.
func grantArgs(ctx context.Context, cat *catalog.Catalog, group, device, token string, read, write bool) (kind, principal, perm string, err error) {
	set := 0
	for _, s := range []string{group, device, token} {
		if s != "" {
			set++
		}
	}
	if set != 1 {
		return "", "", "", errors.New("name exactly one of --group, --device or --read-token")
	}
	if read == write {
		return "", "", "", errors.New("pass exactly one of --read or --write")
	}
	perm = catalog.PermRead
	if write {
		perm = catalog.PermWrite
	}
	switch {
	case group != "":
		return catalog.PrincipalGroup, group, perm, nil
	case token != "":
		if perm != catalog.PermRead {
			return "", "", "", errors.New("a read token only reads; pass --read")
		}
		if !strings.HasPrefix(token, "rtk_") {
			return "", "", "", fmt.Errorf("a read token is named by its id, rtk_…, not %q", token)
		}
		return catalog.PrincipalReadToken, token, perm, nil
	default:
		if perm != catalog.PermWrite {
			return "", "", "", errors.New("a device only writes; pass --write")
		}
		d, err := cat.DeviceByName(ctx, device)
		if errors.Is(err, catalog.ErrNoDevice) {
			return "", "", "", fmt.Errorf("no device named %s; serve devices list shows them", device)
		}
		if err != nil {
			return "", "", "", err
		}
		return catalog.PrincipalDevice, d.ID, perm, nil
	}
}

func listBays(ctx context.Context, env Env, cat *catalog.Catalog) error {
	bays, err := cat.Bays(ctx)
	if err != nil {
		return err
	}
	counts, err := cat.BaySessionCounts(ctx)
	if err != nil {
		return err
	}
	for _, b := range bays {
		var notes []string
		if b.Default {
			notes = append(notes, "default")
			if b.Disabled {
				notes = append(notes, "off")
			}
		}
		if len(b.Aliases) > 0 {
			notes = append(notes, "aliases="+strings.Join(b.Aliases, ","))
		}
		line := fmt.Sprintf("%s id=%s sessions=%d", b.Name, b.ID, counts[b.ID])
		if len(notes) > 0 {
			line += " " + strings.Join(notes, " ")
		}
		fmt.Fprintln(env.stdout(), line)
	}
	return nil
}

func listGrants(ctx context.Context, env Env, cat *catalog.Catalog) error {
	grants, err := cat.Grants(ctx, "", "")
	if err != nil {
		return err
	}
	bays, err := cat.Bays(ctx)
	if err != nil {
		return err
	}
	names := map[string]string{}
	for _, b := range bays {
		names[b.ID] = b.Name
	}
	devices, err := cat.Devices(ctx)
	if err != nil {
		return err
	}
	devNames := map[string]string{}
	for _, d := range devices {
		devNames[d.ID] = d.Name
	}
	for _, g := range grants {
		who := g.Principal
		if g.PrincipalKind == catalog.PrincipalDevice && devNames[who] != "" {
			who = devNames[who]
		}
		fmt.Fprintf(env.stdout(), "%s %s %s %s by=%s at=%s\n", names[g.BayID], g.PrincipalKind, who, g.Permission, g.GrantedBy, g.Granted.UTC().Format(time.RFC3339))
	}
	if len(grants) == 0 {
		fmt.Fprintln(env.stdout(), "no grants")
	}
	return nil
}
