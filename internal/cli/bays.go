package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
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
  terva-lampi serve bays rules [--data DIR]
  terva-lampi serve bays rule BAY (--hold | --add | --deny) MATCH... [--data DIR]
  terva-lampi serve bays unrule ID [--data DIR]
  terva-lampi serve bays holds [--data DIR]
  terva-lampi serve bays release SESSION-UID [--data DIR]
  terva-lampi serve bays inbox [--data DIR]
  terva-lampi serve bays move BAY [--from BAY] FILTER... [--dry-run] [--data DIR]
  terva-lampi serve bays apply-rules [--dry-run] [--data DIR]

MATCH is one or more of --cwd-prefix P, --cwd-glob G, --cwd-hash H,
--git-remote R, --git-remote-prefix R and --harness H. FILTER is one or
more of --project ID, --git-remote R, --git-remote-prefix R, --cwd-prefix
P, --device NAME and --harness H.

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

rule adds a routing rule (docs/policy.md#routing). The lake applies it
to every manifest from then on, sessions already stored included, and
only ever adds. Every MATCH given must match, read exactly as an allow
rule is: --cwd-prefix covers the folder and everything under it, so a
rule for one folder is --cwd-hash. --add also puts the session in BAY.
--deny keeps it out of BAY. --hold puts a new session in BAY alone and
holds the bays it asks for, and flags a stored one for review. unrule
takes a rule away; a hold it placed stays until released. rules lists
them with their ids.

holds lists the sessions held or flagged. release ends a session's hold:
the bays it asked for while held are placed, and a held session leaves
the hold bay.

inbox lists the sessions that need an admin, each with why: in the
default bay with nothing that placed it, a request the lake refused
(which bay, and why), or a hold. The aim is an empty inbox
(docs/bays-inbox.md).

move adds the sessions in --from (default: the default bay) that every
FILTER matches to BAY, and takes them out of --from. apply-rules routes
every stored session again by the rules as they are now: it only adds,
and a hold flags. --dry-run lists what either would change and writes
nothing. Every change is audited.

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
	need := map[string]int{"list": 0, "grants": 0, "rules": 0, "holds": 0, "inbox": 0, "apply-rules": 0, "move": 1, "create": 1, "unalias": 1, "delete": 1, "default": 1, "grant": 1, "revoke": 1, "rule": 1, "unrule": 1, "release": 1, "rename": 2, "alias": 2}
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
	var read, write, yes, hold, add, deny, dryRun bool
	var from, project string
	var match config.ProjectMatch
	var harness string
	rest, err := parseFlags(env, args, baysUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&group, "group", "", "an IdP group")
		fs.StringVar(&device, "device", "", "a device name")
		fs.StringVar(&token, "read-token", "", "a read token id (rtk_…)")
		fs.BoolVar(&read, "read", false, "the read permission")
		fs.BoolVar(&write, "write", false, "the write permission")
		fs.BoolVar(&yes, "yes", false, "confirm delete")
		fs.BoolVar(&hold, "hold", false, "a rule that holds sessions for review")
		fs.BoolVar(&add, "add", false, "a rule that adds sessions to the bay")
		fs.BoolVar(&deny, "deny", false, "a rule that keeps sessions out of the bay")
		fs.StringVar(&match.CWDPrefix, "cwd-prefix", "", "match a cwd at or under this folder")
		fs.StringVar(&match.CWDGlob, "cwd-glob", "", "match a cwd under this directory layout")
		fs.StringVar(&match.CWDHash, "cwd-hash", "", "match exactly this folder, by hash")
		fs.StringVar(&match.GitRemote, "git-remote", "", "match this git remote")
		fs.StringVar(&match.GitRemotePrefix, "git-remote-prefix", "", "match remotes under this owner or host")
		fs.StringVar(&harness, "harness", "", "match this harness")
		fs.BoolVar(&dryRun, "dry-run", false, "list what would change and write nothing")
		fs.StringVar(&from, "from", catalog.DefaultBayName, "the bay move takes sessions out of")
		fs.StringVar(&project, "project", "", "a project id, as the dashboard shows it")
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
	if lister, ok := map[string]func(context.Context, Env, *catalog.Catalog) error{"list": listBays, "grants": listGrants, "rules": listRules, "holds": listHolds, "inbox": listInbox}[sub]; ok {
		cat, err := catalog.OpenReadOnly(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		return lister(ctx, env, cat)
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
		// BAY may be an id or an alias; the name kept as an alias is the
		// bay's own (review 1401).
		b, err := cat.ResolveBay(ctx, pos[0])
		if err != nil {
			return err
		}
		if err := cat.RenameBay(ctx, b.ID, pos[1], actor, now); err != nil {
			return err
		}
		done = fmt.Sprintf("renamed %s to %s; %s stays as an alias", b.Name, pos[1], b.Name)
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
	case "rule":
		action, err := ruleAction(hold, add, deny)
		if err != nil {
			fmt.Fprint(env.stdout(), baysUsage)
			return err
		}
		r, err := cat.AddBayRule(ctx, catalog.BayRule{Match: match, Harness: harness, Action: action, BayID: pos[0]}, actor, now)
		if errors.Is(err, catalog.ErrRuleEmpty) {
			fmt.Fprint(env.stdout(), baysUsage)
		}
		if err != nil {
			return err
		}
		done = fmt.Sprintf("added rule %d: %s", r.ID, ruleLine(r, pos[0]))
	case "unrule":
		id, err := strconv.ParseInt(pos[0], 10, 64)
		if err != nil {
			return fmt.Errorf("serve bays unrule takes a rule id from serve bays rules, not %q", pos[0])
		}
		if err := cat.RemoveBayRule(ctx, id, actor, now); err != nil {
			return err
		}
		done = fmt.Sprintf("removed rule %d", id)
	case "move":
		f := catalog.SessionFilter{Project: project, GitRemote: match.GitRemote, GitRemotePrefix: match.GitRemotePrefix, CWDPrefix: match.CWDPrefix, Harness: harness}
		if device != "" {
			d, err := cat.DeviceByName(ctx, device)
			if err != nil {
				return fmt.Errorf("no device named %s; serve devices list shows them", device)
			}
			f.Device = d.ID
		}
		uids, err := cat.MoveSessions(ctx, catalog.Move{From: from, To: pos[0], Filter: f, Actor: actor, DryRun: dryRun}, now)
		if errors.Is(err, catalog.ErrNoFilter) {
			fmt.Fprint(env.stdout(), baysUsage)
		}
		if err != nil {
			return err
		}
		for _, uid := range uids {
			fmt.Fprintln(env.stdout(), uid)
		}
		if dryRun {
			fmt.Fprintf(env.stdout(), "would move %d sessions from %s to %s; nothing was written\n", len(uids), from, pos[0])
			return nil
		}
		done = fmt.Sprintf("moved %d sessions from %s to %s", len(uids), from, pos[0])
	case "apply-rules":
		applied, err := cat.ApplyRules(ctx, actor, dryRun, now)
		if err != nil {
			return err
		}
		names, err := bayNames(ctx, cat)
		if err != nil {
			return err
		}
		for _, a := range applied {
			verb := "added to"
			if a.Action == catalog.RuleHold {
				verb = "flagged for"
			}
			fmt.Fprintf(env.stdout(), "%s %s %s by rule %d\n", a.SessionUID, verb, names[a.BayID], a.RuleID)
		}
		if dryRun {
			fmt.Fprintf(env.stdout(), "would make %d changes; nothing was written\n", len(applied))
			return nil
		}
		done = fmt.Sprintf("applied the rules: %d changes", len(applied))
	case "release":
		if err := cat.ReleaseHold(ctx, pos[0], actor, now); err != nil {
			return err
		}
		done = "released " + pos[0]
	}
	// The change is committed. Say so first, so a failed audit write is
	// not read as a change that did not happen.
	fmt.Fprintln(env.stdout(), done)
	if err := cat.FlushAudit(ctx, data); err != nil {
		return fmt.Errorf("%s, but writing it to %s failed: %w; the change stands, and the line stays queued until the audit log can be written", done, audit.FileName, err)
	}
	return nil
}

func ruleAction(hold, add, deny bool) (string, error) {
	var set []string
	for name, on := range map[string]bool{catalog.RuleHold: hold, catalog.RuleAdd: add, catalog.RuleDeny: deny} {
		if on {
			set = append(set, name)
		}
	}
	if len(set) != 1 {
		return "", errors.New("pass exactly one of --hold, --add or --deny")
	}
	return set[0], nil
}

// ruleLine is a rule as rules prints it, with its bay called bay.
func ruleLine(r catalog.BayRule, bay string) string {
	parts := []string{r.Action, bay, "when"}
	for _, f := range []struct{ flag, v string }{
		{"cwd-prefix", r.Match.CWDPrefix}, {"cwd-glob", r.Match.CWDGlob}, {"cwd-hash", r.Match.CWDHash},
		{"git-remote", r.Match.GitRemote}, {"git-remote-prefix", r.Match.GitRemotePrefix}, {"harness", r.Harness},
	} {
		if f.v != "" {
			parts = append(parts, f.flag+"="+f.v)
		}
	}
	return strings.Join(parts, " ")
}

func bayNames(ctx context.Context, cat *catalog.Catalog) (map[string]string, error) {
	bays, err := cat.Bays(ctx)
	if err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, b := range bays {
		names[b.ID] = b.Name
	}
	return names, nil
}

func listRules(ctx context.Context, env Env, cat *catalog.Catalog) error {
	rules, err := cat.BayRules(ctx)
	if err != nil {
		return err
	}
	names, err := bayNames(ctx, cat)
	if err != nil {
		return err
	}
	for _, r := range rules {
		fmt.Fprintf(env.stdout(), "%d %s by=%s at=%s\n", r.ID, ruleLine(r, names[r.BayID]), r.CreatedBy, r.Created.UTC().Format(time.RFC3339))
	}
	if len(rules) == 0 {
		fmt.Fprintln(env.stdout(), "no rules")
	}
	return nil
}

func listInbox(ctx context.Context, env Env, cat *catalog.Catalog) error {
	entries, err := cat.Inbox(ctx)
	if err != nil {
		return err
	}
	names, err := bayNames(ctx, cat)
	if err != nil {
		return err
	}
	for _, e := range entries {
		var bays []string
		for _, id := range e.Bays {
			bays = append(bays, names[id])
		}
		where := e.CWD
		if e.GitRemote != "" {
			where += " " + e.GitRemote
		}
		fmt.Fprintf(env.stdout(), "%s %s %s %s bays=%s\n", e.SessionUID, e.Harness, e.NativeID, where, strings.Join(bays, ","))
		for _, r := range e.Reasons {
			fmt.Fprintf(env.stdout(), "  %s\n", r)
		}
	}
	if len(entries) == 0 {
		fmt.Fprintln(env.stdout(), "the inbox is empty")
	}
	return nil
}

func listHolds(ctx context.Context, env Env, cat *catalog.Catalog) error {
	holds, err := cat.Holds(ctx)
	if err != nil {
		return err
	}
	names, err := bayNames(ctx, cat)
	if err != nil {
		return err
	}
	for _, h := range holds {
		fmt.Fprintf(env.stdout(), "%s %s bay=%s rule=%d at=%s\n", h.SessionUID, h.State, names[h.BayID], h.RuleID, h.Created.UTC().Format(time.RFC3339))
	}
	if len(holds) == 0 {
		fmt.Fprintln(env.stdout(), "no holds")
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
	names, err := bayNames(ctx, cat)
	if err != nil {
		return err
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
