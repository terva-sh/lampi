package web

import (
	"cmp"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// Merge selected: the operator ticks allow rules, the editor proposes the
// one rule that covers them, their common folder or remote owner, and
// shows what that rule would do before anything changes: the rules it
// replaces, ticked or not, a ticked rule it misses, and the projects it
// admits that no rule admits now. The operator can edit the prefix and
// check again, then apply it to the form and preview as usual. See
// TKT-01M6ETH6H726EM1DJJS98M56JV.

// maxMergeShown bounds the projects the merge panel lists.
const maxMergeShown = 200

// mergeProject is one project a merge would admit that no rule admits
// now.
type mergeProject struct {
	Key      catalog.ProjectKey
	Devices  []string
	Sessions int
	Newest   time.Time
	// Hidden is a project hidden from review: an operator decided not
	// to import it, and the merge would import it.
	Hidden bool
}

// mergeView is the merge panel.
type mergeView struct {
	// Kind is the field the merged rule sets, cwd_prefix or
	// git_remote_prefix, and Prefix its value.
	Kind, Prefix string
	Picked       []string
	Problem      string
	RuleText     string
	Replaces     []string
	Missed       []string
	Admits       []mergeProject
	AdmitsMore   int
	Hidden       int
	Unlisted     []string
	Before       int
	After        int
	rule         config.ProjectMatch
}

// mergeKind is the field Merge would widen for rule m: a folder rule's
// cwd_prefix, or a repository's or remote prefix's git_remote_prefix;
// "" for a rule with more than one field, or another kind.
func mergeKind(m config.ProjectMatch) (kind, value string) {
	conds := condsOf(m)
	if len(conds) != 1 {
		return "", ""
	}
	switch conds[0].Kind {
	case "cwd_prefix":
		return "cwd_prefix", strings.TrimSuffix(conds[0].Value, "/")
	case "git_remote", "git_remote_prefix":
		return "git_remote_prefix", config.NormalizeRemote(conds[0].Value)
	}
	return "", ""
}

// commonPrefix is the deepest folder or remote path that holds every
// value: a value itself when it holds the rest.
func commonPrefix(values []string) string {
	split := func(s string) []string { return strings.Split(strings.Trim(s, "/"), "/") }
	common := split(values[0])
	for _, v := range values[1:] {
		segs := split(v)
		n := 0
		for n < len(common) && n < len(segs) && common[n] == segs[n] {
			n++
		}
		common = common[:n]
	}
	return strings.Join(common, "/")
}

// planMerge works out what merging picked into one rule does to p's
// allow rules for the devices on profile name. prefix is the operator's
// edit of the proposal, or "" to propose one.
func (s *Server) planMerge(r *http.Request, name string, p config.Projects, picked []config.ProjectMatch, prefix string) (mergeView, error) {
	var v mergeView
	for _, m := range picked {
		v.Picked = append(v.Picked, ruleText(m))
	}
	if len(picked) < 2 {
		v.Problem = "Tick two or more allow rules to merge."
		return v, nil
	}
	var values []string
	for _, m := range picked {
		kind, value := mergeKind(m)
		if kind == "" || v.Kind != "" && kind != v.Kind {
			v.Problem = "Merge takes folder rules, or repository and remote prefix rules, each with one condition. Tick rules of one kind."
			return v, nil
		}
		v.Kind = kind
		values = append(values, value)
	}
	v.Prefix = strings.TrimSpace(prefix)
	if v.Prefix == "" {
		v.Prefix = commonPrefix(values)
		if v.Kind == "cwd_prefix" {
			v.Prefix = "/" + v.Prefix
		}
	}
	switch v.Kind {
	case "cwd_prefix":
		v.Prefix = path.Clean(v.Prefix)
		if !strings.HasPrefix(v.Prefix, "/") || !profiledoctor.WideFolder(v.Prefix) {
			v.Problem = "Merge does not offer " + v.Prefix + ": a rule for it would admit a home directory, the folder holding home directories, a top-level folder, or the whole machine. Choose a folder below it."
			return v, nil
		}
		v.rule = config.ProjectMatch{CWDPrefix: v.Prefix}
	default:
		v.Prefix = config.NormalizeRemote(v.Prefix)
		if !strings.Contains(v.Prefix, "/") {
			v.Problem = "Merge does not offer " + cmp.Or(v.Prefix, "an empty prefix") + ": a rule for a whole host admits every repository on it. Choose an owner on the host, such as host/owner."
			return v, nil
		}
		v.rule = config.ProjectMatch{GitRemotePrefix: v.Prefix}
	}
	v.RuleText = ruleText(v.rule)
	next := p
	var removed []config.ProjectMatch
	next.Allow, removed = withRules(p.Allow, []config.ProjectMatch{v.rule})
	for _, m := range removed {
		v.Replaces = append(v.Replaces, ruleText(m))
	}
	for _, m := range picked {
		if !config.Covers(v.rule, m) {
			v.Missed = append(v.Missed, ruleText(m))
		}
	}
	v.Before, v.After = len(p.Allow), len(next.Allow)

	in, err := profiledoctor.Gather(r.Context(), s.catalog, name, p)
	if err != nil {
		return v, err
	}
	byKey := map[catalog.ProjectKey]*mergeProject{}
	var order []catalog.ProjectKey
	for _, d := range in.Devices {
		if !d.Listed {
			v.Unlisted = append(v.Unlisted, d.Name)
		}
		for _, ip := range d.Projects {
			k, ok := catalog.ProjectKeyOf(ip)
			id := config.ProjectID{CWD: ip.CWD, CWDHash: ip.CWDHash, GitRemote: ip.GitRemote}
			if !ok || p.Permitted(id) || !next.Permitted(id) {
				continue
			}
			mp := byKey[k]
			if mp == nil {
				mp = &mergeProject{Key: k, Hidden: in.Hidden[k]}
				byKey[k] = mp
				order = append(order, k)
			}
			if !slices.Contains(mp.Devices, d.Name) {
				mp.Devices = append(mp.Devices, d.Name)
			}
			mp.Sessions += ip.Sessions
			if ip.Newest.After(mp.Newest) {
				mp.Newest = ip.Newest
			}
		}
	}
	for _, k := range order {
		if byKey[k].Hidden {
			v.Hidden++
		}
		v.Admits = append(v.Admits, *byKey[k])
	}
	// Hidden first, since those are the ones someone decided against.
	slices.SortStableFunc(v.Admits, func(a, b mergeProject) int {
		if a.Hidden != b.Hidden {
			if a.Hidden {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Sessions, a.Sessions), cmp.Compare(a.Key.Key, b.Key.Key))
	})
	if len(v.Admits) > maxMergeShown {
		v.AdmitsMore = len(v.Admits) - maxMergeShown
		v.Admits = v.Admits[:maxMergeShown]
	}
	return v, nil
}

// mergeForm handles the editor's Merge buttons: merge=allow plans the
// merge of the ticked rules and shows it, and merge_apply=allow applies
// the planned rule to the form. It returns the panel to show, or nil
// when the merge was applied or not asked for.
func (s *Server) mergeForm(r *http.Request, name string, f *profileForm, p *config.Profile) (*mergeView, error) {
	plan, apply := r.PostForm.Get("merge") == "allow", r.PostForm.Get("merge_apply") == "allow"
	if !plan && !apply {
		return nil, nil
	}
	v, err := s.planMerge(r, name, p.Projects, f.Picked, r.PostForm.Get("merge_prefix"))
	if err != nil || !apply || v.Problem != "" {
		return &v, err
	}
	p.Projects.Allow, _ = withRules(p.Projects.Allow, []config.ProjectMatch{v.rule})
	f.Allow, f.Picked = withBlanks(p.Projects.Allow), nil
	if f.Note == "" {
		n := len(v.Replaces)
		f.Note = "Merge " + strconv.Itoa(n) + " allow " + plural(n, "rule", "rules") + " into " + v.RuleText
	}
	return nil, nil
}
