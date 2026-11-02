package web

import (
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/profiledoctor"
)

// The editor's rule list: one line per rule, a kind and a value for each
// field the rule sets, what the rule admits on the devices, and the
// doctor's findings on it. Rules that share a folder or owner sit under
// one header. See TKT-01M6ETZV536S1Z3SP2D5KZ4GJX.

// maxConditions bounds the conditions one rule's form posts: one per
// field of config.ProjectMatch.
const maxConditions = 5

// ruleKind is one field a rule can set, as the kind picker names it.
type ruleKind struct {
	ID, Label, Placeholder string
}

var ruleKinds = []ruleKind{
	{"cwd_prefix", "Folder", "/home/me/work"},
	{"git_remote", "Repository", "git@github.com:org/repo"},
	{"git_remote_prefix", "Remote prefix", "github.com/org"},
	{"cwd_hash", "Folder hash", "16 hex digits"},
	{"cwd_glob", "Folder pattern", "/home/*/notes"},
}

// ruleCond is one field a rule sets.
type ruleCond struct {
	Kind, Value string
}

// ruleRowView is one rule on the editor's list.
type ruleRowView struct {
	List  string
	Index int
	Conds []ruleCond
	// Blank is an empty row for a new rule.
	Blank bool
	// Group names the folder or owner of the run of rules this one
	// starts, and GroupCount counts them; empty when it starts none.
	Group      string
	GroupCount int
	// Use is what the rule admits, for allow rules when the devices
	// could be read.
	Use      *profiledoctor.RuleUse
	Findings []healthItem
}

// fieldOf is the field of m kind names.
func fieldOf(m *config.ProjectMatch, kind string) (*string, bool) {
	switch kind {
	case "cwd_prefix":
		return &m.CWDPrefix, true
	case "git_remote":
		return &m.GitRemote, true
	case "git_remote_prefix":
		return &m.GitRemotePrefix, true
	case "cwd_hash":
		return &m.CWDHash, true
	case "cwd_glob":
		return &m.CWDGlob, true
	}
	return nil, false
}

// condsOf lists the fields m sets, in the kind picker's order.
func condsOf(m config.ProjectMatch) []ruleCond {
	var out []ruleCond
	for _, k := range ruleKinds {
		if f, _ := fieldOf(&m, k.ID); *f != "" {
			out = append(out, ruleCond{k.ID, *f})
		}
	}
	return out
}

// readRule reads row prefix p of a rules form: the field names the
// editor used to post, list.N.FIELD, then the conditions list.N.c.M.kind
// and list.N.c.M.value it posts now.
func readRule(v url.Values, p string) (config.ProjectMatch, error) {
	m := config.ProjectMatch{CWDPrefix: v.Get(p + "cwd_prefix"), GitRemote: v.Get(p + "git_remote"),
		GitRemotePrefix: v.Get(p + "git_remote_prefix"), CWDHash: v.Get(p + "cwd_hash"), CWDGlob: v.Get(p + "cwd_glob")}
	for c := range maxConditions {
		q := p + "c." + strconv.Itoa(c) + "."
		kind, value := v.Get(q+"kind"), strings.TrimSpace(v.Get(q+"value"))
		if value == "" {
			continue
		}
		f, ok := fieldOf(&m, kind)
		if !ok {
			return m, fmt.Errorf("choose what %q names: a folder, repository, remote prefix, folder hash or folder pattern", value)
		}
		if *f != "" && strings.TrimSpace(*f) != value {
			return m, fmt.Errorf("one rule sets %s twice, to %q and %q; every field set on a rule must match, so give each its own rule", kind, *f, value)
		}
		*f = value
	}
	return m, nil
}

// groupKey is the folder or owner a rule is listed under: the folder
// above a folder rule, the owner of a repository rule, the host of a
// remote prefix. Empty for a rule with more than one field or another
// kind.
func groupKey(m config.ProjectMatch) string {
	conds := condsOf(m)
	if len(conds) != 1 {
		return ""
	}
	v := strings.TrimSuffix(conds[0].Value, "/")
	switch conds[0].Kind {
	case "cwd_prefix", "git_remote", "git_remote_prefix":
		if d := path.Dir(v); d != "." && d != "/" {
			return d
		}
	}
	return ""
}

// ruleRows lays rules out for the editor, with what h says about each
// allow rule.
func ruleRows(list string, rules []config.ProjectMatch, h *healthView) []ruleRowView {
	rows := make([]ruleRowView, len(rules))
	for i, r := range rules {
		rows[i] = ruleRowView{List: list, Index: i, Conds: condsOf(r)}
		if len(rows[i].Conds) == 0 {
			rows[i].Blank = true
			rows[i].Conds = []ruleCond{{Kind: "cwd_prefix"}}
		}
		if h == nil || list != "allow" {
			continue
		}
		if i < len(h.Uses) {
			rows[i].Use = &h.Uses[i]
		}
		for _, it := range h.Items {
			// Unmatched says what Use already shows.
			if it.Rule != nil && it.Index == i && it.Code != profiledoctor.CodeUnmatched {
				rows[i].Findings = append(rows[i].Findings, it)
			}
		}
	}
	for i := 0; i < len(rows); {
		key := groupKey(rules[i])
		j := i + 1
		for key != "" && j < len(rows) && groupKey(rules[j]) == key {
			j++
		}
		if j-i > 1 {
			rows[i].Group, rows[i].GroupCount = key, j-i
		}
		i = j
	}
	return rows
}

// ruleCondView is one condition's picker and value in the form.
type ruleCondView struct {
	Name, Label, Kind, Value, Placeholder string
}

func newRuleCondView(row ruleRowView, m int, c ruleCond) ruleCondView {
	if c.Kind == "" {
		c.Kind = "cwd_prefix"
	}
	v := ruleCondView{Name: fmt.Sprintf("%s.%d.c.%d", row.List, row.Index, m), Kind: c.Kind, Value: c.Value,
		Label: fmt.Sprintf("%s rule %d condition %d", row.List, row.Index+1, m+1)}
	for _, k := range ruleKinds {
		if k.ID == c.Kind {
			v.Placeholder = k.Placeholder
		}
	}
	return v
}
