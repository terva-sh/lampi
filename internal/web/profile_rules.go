package web

import (
	"cmp"
	"fmt"
	"net/url"
	"path"
	"slices"
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
	// Picked is an allow rule ticked for Merge.
	Picked bool
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
// and list.N.c.M.value it posts now. It also returns every condition as
// sent, which the editor shows again when the rule cannot be read.
func readRule(v url.Values, p string) (config.ProjectMatch, []ruleCond, error) {
	m := config.ProjectMatch{CWDPrefix: v.Get(p + "cwd_prefix"), GitRemote: v.Get(p + "git_remote"),
		GitRemotePrefix: v.Get(p + "git_remote_prefix"), CWDHash: v.Get(p + "cwd_hash"), CWDGlob: v.Get(p + "cwd_glob")}
	sent := condsOf(m)
	var err error
	// A rule shown again as sent can hold the old field names and its
	// conditions both, so read twice as many as a rule can set.
	for c := range 2 * maxConditions {
		q := p + "c." + strconv.Itoa(c) + "."
		kind, value := v.Get(q+"kind"), strings.TrimSpace(v.Get(q+"value"))
		if value == "" {
			continue
		}
		sent = append(sent, ruleCond{kind, value})
		if err != nil {
			continue
		}
		f, ok := fieldOf(&m, kind)
		switch {
		case !ok:
			err = fmt.Errorf("choose what %q names: a folder, repository, remote prefix, folder hash or folder pattern", value)
		case *f != "" && strings.TrimSpace(*f) != value:
			err = fmt.Errorf("one rule sets %s twice, to %q and %q; every field set on a rule must match, so give each its own rule", kind, *f, value)
		default:
			*f = value
		}
	}
	return m, sent, err
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
// allow rule. raw holds the conditions as sent for the rows the form
// could not read, which show again as they were.
func ruleRows(list string, rules []config.ProjectMatch, raw map[int][]ruleCond, h *healthView) []ruleRowView {
	rows := make([]ruleRowView, len(rules))
	// h reads the rules normalized, which drops empty rows: n is each
	// row's index there.
	n := 0
	for i, r := range rules {
		rows[i] = ruleRowView{List: list, Index: i, Conds: condsOf(r)}
		if c, ok := raw[i]; ok {
			rows[i].Conds = c
		}
		if len(rows[i].Conds) == 0 {
			rows[i].Blank = true
			rows[i].Conds = []ruleCond{{Kind: "cwd_prefix"}}
		}
		if len(normalizeRules([]config.ProjectMatch{r})) == 0 {
			continue
		}
		at := n
		n++
		if h == nil || list != "allow" {
			continue
		}
		if at < len(h.Uses) {
			rows[i].Use = &h.Uses[at]
		}
		for _, it := range h.Items {
			// Unmatched says what Use already shows.
			if it.Rule != nil && it.Index == at && it.Code != profiledoctor.CodeUnmatched {
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

// reorder applies the form's Move and Sort buttons to rows, the rules of
// list as posted. Order changes nothing an agent does: any deny rule
// refuses, then any allow rule admits. It is the operator's to set. See
// TKT-01M6ETZV7C0P5F2RDKQPAQCCFW.
func reorder(v url.Values, list string, rows []config.ProjectMatch) []config.ProjectMatch {
	if mv, ok := strings.CutPrefix(v.Get("move"), list+"."); ok {
		n, dir, _ := strings.Cut(mv, ".")
		i, err := strconv.Atoi(n)
		j := i - 1
		if dir == "down" {
			j = i + 1
		}
		if err == nil && (dir == "up" || dir == "down") && i >= 0 && i < len(rows) && j >= 0 && j < len(rows) {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	if v.Get("sort") == list {
		slices.SortStableFunc(rows, func(a, b config.ProjectMatch) int { return cmp.Compare(sortKey(a), sortKey(b)) })
	}
	return rows
}

// sortKey orders rules for Sort: folder rules by path, then repository
// and remote prefix rules by remote, so each folder's and owner's rules
// sit together; empty rows last.
func sortKey(m config.ProjectMatch) string {
	conds := condsOf(m)
	if len(conds) == 0 {
		return "9"
	}
	v := strings.TrimSuffix(strings.TrimSpace(conds[0].Value), "/")
	switch conds[0].Kind {
	case "cwd_prefix", "cwd_glob", "cwd_hash":
		return "0" + v + "\x00" + conds[0].Kind
	}
	return "1" + config.NormalizeRemote(v) + "\x00" + conds[0].Kind
}
