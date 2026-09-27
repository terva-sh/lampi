package upload

import (
	"sort"
	"strings"

	"terva.sh/lampi/internal/config"
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

// Refusals reads the harness homes in opt the way Sync does and groups
// the sessions opt.Projects refuses by project: the folded remote when
// there is one, since allow rules name repositories, and otherwise the
// cwd. Within a repository the reason is the first session's; sessions
// of one repository in different cwds can differ only when a cwd rule
// is involved, and the report is a guide to writing rules, not a
// verdict per session.
// It sends nothing, opens no sync state, and snapshots no refused Cursor
// session. skipped names what could not be read, as in Result.
func Refusals(opt Options) (projects []RefusedProject, skipped []string) {
	bundles, skipped := bundlesFor(opt)
	defer cleanupBundles(bundles)
	byKey := map[string]*RefusedProject{}
	harnesses := map[string]map[string]bool{}
	cwds := map[string]map[string]bool{}
	for _, b := range bundles {
		for _, m := range b.Manifests {
			id := projectID(m)
			reason := opt.Projects.Refusal(id)
			if reason == "" {
				continue
			}
			key := "cwd\x00" + id.CWD
			if r := config.NormalizeRemote(id.GitRemote); r != "" {
				key = "remote\x00" + r
			}
			p := byKey[key]
			if p == nil {
				p = &RefusedProject{GitRemote: id.GitRemote, Reason: reason}
				byKey[key] = p
				harnesses[key] = map[string]bool{}
				cwds[key] = map[string]bool{}
			}
			p.Sessions++
			harnesses[key][m.Harness] = true
			cwds[key][id.CWD] = true
		}
	}
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
		p.CWD, p.CWDs = all[0], len(all)
		projects = append(projects, *p)
	}
	sort.Slice(projects, func(i, j int) bool {
		a, b := projects[i], projects[j]
		if a.Sessions != b.Sessions {
			return a.Sessions > b.Sessions
		}
		if a.CWD != b.CWD {
			return a.CWD < b.CWD
		}
		return strings.Compare(a.GitRemote, b.GitRemote) < 0
	})
	return projects, skipped
}
