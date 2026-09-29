package upload

import (
	"sort"
	"strings"
	"time"

	"terva.sh/lampi/internal/adapter"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// RefusedProject is one project whose sessions the allowlist keeps on
// this machine. A project with a git remote is that repository, however
// many checkouts it has: CWD is the first cwd in sort order and CWDs
// counts them. One without a remote is its cwd. GitRemote is the remote
// as the manifest records it, with any user part and password removed.
type RefusedProject struct {
	CWD       string
	CWDs      int
	GitRemote string
	Harnesses []string
	Sessions  int
	Reason    string
}

// InventoryRow is one project on this machine, allowed or refused,
// grouped as Refusals groups them. CWDHash is CWD's. Bytes sums the
// sessions' artifact sizes and Newest is the newest artifact's
// modification time. Reason is empty for an allowed project, and
// otherwise why the allowlist refuses it.
type InventoryRow struct {
	CWD       string
	CWDs      int
	CWDHash   string
	GitRemote string
	Harnesses []string
	Sessions  int
	Bytes     int64
	Newest    time.Time
	Reason    string
}

// Refusals reads the harness homes in opt the way Sync does and groups
// the sessions opt.Projects refuses by project and reason: the folded
// remote when there is one, since allow rules name repositories, and
// otherwise the cwd. Checkouts of one repository refused for different
// reasons, such as one under a deny rule, are separate lines, so every
// line's reason is true of every session it counts.
// It sends nothing, opens no sync state, and snapshots no refused Cursor
// session. skipped names what could not be read, as in Result.
func Refusals(opt Options) (projects []RefusedProject, skipped []string) {
	bundles, skipped := bundlesFor(opt)
	defer cleanupBundles(bundles)
	for _, r := range inventoryRows(opt, bundles) {
		if r.Reason == "" {
			continue
		}
		projects = append(projects, RefusedProject{
			CWD: r.CWD, CWDs: r.CWDs, GitRemote: r.GitRemote,
			Harnesses: r.Harnesses, Sessions: r.Sessions, Reason: r.Reason,
		})
	}
	return projects, skipped
}

// Narrowed lists the projects on this machine that from allows and to
// refuses, grouped as Refusals groups them, with to's reason. It reads
// what Refusals reads, once, and uploads nothing. A switch from one
// allowlist to another stops uploading exactly these.
func Narrowed(opt Options, from, to config.Projects) (projects []RefusedProject, skipped []string) {
	opt.Projects = from
	bundles, skipped := bundlesFor(opt)
	defer cleanupBundles(bundles)
	kept := make([]adapter.Bundle, 0, len(bundles))
	for _, b := range bundles {
		var ms []protocol.Manifest
		for _, m := range b.Manifests {
			if from.Permitted(projectID(m)) {
				ms = append(ms, m)
			}
		}
		b.Manifests, b.Cleanup = ms, nil
		kept = append(kept, b)
	}
	opt.Projects = to
	for _, r := range inventoryRows(opt, kept) {
		if r.Reason == "" {
			continue
		}
		projects = append(projects, RefusedProject{
			CWD: r.CWD, CWDs: r.CWDs, GitRemote: r.GitRemote,
			Harnesses: r.Harnesses, Sessions: r.Sessions, Reason: r.Reason,
		})
	}
	return projects, skipped
}

// PermittedByAny lists the projects on this machine with a session that
// any of rules allows, grouped as Refusals groups them, reading once. A
// project two rule sets allow in different checkouts is one row with
// every session and checkout. It uploads nothing.
func PermittedByAny(opt Options, rules []config.Projects) []RefusedProject {
	opt.Projects = config.Projects{}
	bundles, _ := bundlesFor(opt)
	defer cleanupBundles(bundles)
	kept := make([]adapter.Bundle, 0, len(bundles))
	for _, b := range bundles {
		var ms []protocol.Manifest
		for _, m := range b.Manifests {
			for _, r := range rules {
				if r.Permitted(projectID(m)) {
					ms = append(ms, m)
					break
				}
			}
		}
		b.Manifests, b.Cleanup = ms, nil
		kept = append(kept, b)
	}
	// With no rule every kept session has the same verdict, so each
	// project is one row.
	var projects []RefusedProject
	for _, r := range inventoryRows(opt, kept) {
		projects = append(projects, RefusedProject{
			CWD: r.CWD, CWDs: r.CWDs, GitRemote: r.GitRemote,
			Harnesses: r.Harnesses, Sessions: r.Sessions,
		})
	}
	return projects
}

// inventoryRows groups every session in bundles by project and verdict,
// as Refusals describes, with the allowed ones grouped the same way. It
// is never nil, so a caller can tell an empty machine from one not read.
func inventoryRows(opt Options, bundles []adapter.Bundle) []InventoryRow {
	byKey := map[string]*InventoryRow{}
	harnesses := map[string]map[string]bool{}
	cwds := map[string]map[string]string{}
	for _, b := range bundles {
		for _, m := range b.Manifests {
			id := projectID(m)
			reason := opt.Projects.Refusal(id)
			key := "cwd\x00" + id.CWD
			if r := config.NormalizeRemote(id.GitRemote); r != "" {
				key = "remote\x00" + r
			}
			key += "\x00" + reason
			p := byKey[key]
			if p == nil {
				p = &InventoryRow{GitRemote: id.GitRemote, Reason: reason}
				byKey[key] = p
				harnesses[key] = map[string]bool{}
				cwds[key] = map[string]string{}
			}
			p.Sessions++
			for _, a := range m.Artifacts {
				p.Bytes += a.Size
				if a.MTime.After(p.Newest) {
					p.Newest = a.MTime
				}
			}
			harnesses[key][m.Harness] = true
			cwds[key][id.CWD] = id.CWDHash
		}
	}
	rows := make([]InventoryRow, 0, len(byKey))
	for key, p := range byKey {
		for h := range harnesses[key] {
			p.Harnesses = append(p.Harnesses, h)
		}
		sort.Strings(p.Harnesses)
		var all []string
		for c := range cwds[key] {
			all = append(all, c)
		}
		sort.Strings(all)
		p.CWD, p.CWDs, p.CWDHash = all[0], len(all), cwds[key][all[0]]
		rows = append(rows, *p)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Sessions != b.Sessions {
			return a.Sessions > b.Sessions
		}
		if a.CWD != b.CWD {
			return a.CWD < b.CWD
		}
		if c := strings.Compare(a.GitRemote, b.GitRemote); c != 0 {
			return c < 0
		}
		return a.Reason < b.Reason
	})
	return rows
}

// Inventory is rows as the lake takes them, in mode. A strict
// inventory lists the allowed projects only; either mode totals the
// refused sessions and their bytes. Remotes are folded as allow rules
// fold them. Past protocol.MaxInventoryProjects the rest are left out
// and Truncated is set.
func Inventory(mode string, rows []InventoryRow, at time.Time) protocol.AgentInventory {
	inv := protocol.AgentInventory{Mode: mode, GeneratedAt: at.UTC(), Projects: []protocol.InventoryProject{}}
	for _, r := range rows {
		allowed := r.Reason == ""
		if !allowed {
			inv.RefusedSessions += r.Sessions
			inv.RefusedBytes += r.Bytes
			if mode == protocol.InventoryStrict {
				continue
			}
		}
		if len(inv.Projects) == protocol.MaxInventoryProjects {
			inv.Truncated = true
			continue
		}
		p := protocol.InventoryProject{
			GitRemote: config.NormalizeRemote(r.GitRemote),
			CWD:       r.CWD, CWDs: r.CWDs, CWDHash: r.CWDHash,
			Harnesses: r.Harnesses, Sessions: r.Sessions, Bytes: r.Bytes,
			Allowed: allowed, Reason: r.Reason,
		}
		if !r.Newest.IsZero() {
			p.Newest = r.Newest.UTC()
		}
		inv.Projects = append(inv.Projects, p)
	}
	return inv
}
