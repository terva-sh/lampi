package profiledoctor

import (
	"cmp"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// Device is one device a profile's allow rules reach, with the projects
// its newest inventory lists. A device whose config.json sets its own
// allow rules is not one: the profile's do not reach it.
type Device struct {
	Name string
	// Listed is false when the device sent no inventory, so nothing is
	// known of what it holds.
	Listed   bool
	Projects []protocol.InventoryProject
}

// Input is what the doctor reads: the rules, the devices they reach,
// and the projects the operator hid from review.
type Input struct {
	Projects config.Projects
	Review   config.ReviewConfig
	Devices  []Device
	Hidden   map[catalog.ProjectKey]bool
}

// The fixes a finding offers.
const (
	// FixSafe removes the rule, and no project on any device changes
	// verdict: it is covered, shadowed by a deny rule, or a temporary
	// folder no listed device holds.
	FixSafe = "safe"
	// FixNarrow replaces the rule with exact rules for what it admits
	// today, or for a temporary folder, removes it and hides what it
	// admitted from review. Nothing uploading now stops, except a
	// temporary folder's projects.
	FixNarrow = "narrow"
	// FixRemove removes a rule that admits nothing today. It is not safe:
	// the rule may be for a project nobody has opened yet.
	FixRemove = "remove"
	// FixWiden replaces several rules with one wider one, which admits
	// more than they did. The preview lists what.
	FixWiden = "widen"
	// FixNone is a finding to read, with no change to make here.
	FixNone = "none"
)

// The levels a finding has, worst first.
const (
	LevelProblem = "problem"
	LevelWarning = "warning"
	LevelTidy    = "tidy"
	LevelOffer   = "offer"
	LevelInfo    = "info"
)

// The codes a finding has.
const (
	CodeHomeFolder   = "home_folder"
	CodeAtCap        = "at_cap"
	CodeBroadFolder  = "broad_folder"
	CodeHostWide     = "host_wide"
	CodeNearCap      = "near_cap"
	CodeCovered      = "covered"
	CodeShadowed     = "shadowed"
	CodeTemporary    = "temporary"
	CodeUnmatched    = "unmatched"
	CodeOwnerFold    = "owner_fold"
	CodeFolderFold   = "folder_fold"
	CodeReviewGroup  = "review_group"
	CodeUnlisted     = "unlisted_devices"
	nearCapPercent   = 80
	maxGroupsShown   = 10
	maxAdmitsPerRule = 50
)

// Finding is one thing the doctor would change, or wants read.
type Finding struct {
	Code  string `json:"code"`
	Level string `json:"level"`
	Fix   string `json:"fix"`
	// Index is the allow rule's place in the list, or -1 for a finding
	// about no single rule.
	Index int                  `json:"index"`
	Rule  *config.ProjectMatch `json:"rule,omitempty"`
	// By is the rule that covers or shadows Rule.
	By *config.ProjectMatch `json:"by,omitempty"`
	// Group is a fold's or a review group's owner or folder, and Count
	// how many rules or projects it holds.
	Group string `json:"group,omitempty"`
	Count int    `json:"count,omitempty"`
	// Admits lists the projects Rule admits on the devices today.
	Admits []Project `json:"admits,omitempty"`
	// Replace is what FixNarrow puts in Rule's place, and Hide the
	// projects it hides from review.
	Replace []config.ProjectMatch `json:"replace,omitempty"`
	Hide    []catalog.ProjectKey  `json:"hide,omitempty"`
	Message string                `json:"message"`
}

// Project is a project a rule admits, across the devices that hold it.
type Project struct {
	Key      catalog.ProjectKey `json:"key"`
	Devices  []string           `json:"devices"`
	Sessions int                `json:"sessions"`
	Newest   time.Time          `json:"newest,omitzero"`
	// rule is the exact rule FixNarrow writes for the project.
	rule config.ProjectMatch
}

// Report is the doctor's reading of one profile.
type Report struct {
	Allow int `json:"allow"`
	Deny  int `json:"deny"`
	Cap   int `json:"cap"`
	// Pending counts the refused projects waiting in review on the
	// listed devices.
	Pending int `json:"pending"`
	// Unlisted names the devices that sent no inventory.
	Unlisted []string  `json:"unlisted"`
	Findings []Finding `json:"findings"`
	// Uses is what each allow rule admits on the listed devices, by the
	// rule's index.
	Uses []RuleUse `json:"uses"`
}

// RuleUse is what one allow rule admits: projects it matches that no
// deny rule refuses, and their sessions. A project another rule also
// admits counts for both.
type RuleUse struct {
	Projects int `json:"projects"`
	Sessions int `json:"sessions"`
}

// Safe counts the findings ApplySafe would fix.
func (r Report) Safe() int {
	n := 0
	for _, f := range r.Findings {
		if f.Fix == FixSafe {
			n++
		}
	}
	return n
}

// Diagnose reads in and reports what to change. It changes nothing.
func Diagnose(in Input) Report {
	allow, deny := in.Projects.Allow, in.Projects.Deny
	rep := Report{Allow: len(allow), Deny: len(deny), Cap: RuleCap, Unlisted: []string{}, Findings: []Finding{}}
	for _, d := range in.Devices {
		if !d.Listed {
			rep.Unlisted = append(rep.Unlisted, d.Name)
		}
	}
	admits := admitted(in)
	rep.Uses = make([]RuleUse, len(allow))
	for i, ps := range admits {
		rep.Uses[i].Projects = len(ps)
		for _, p := range ps {
			rep.Uses[i].Sessions += p.Sessions
		}
	}
	covered := CoveredBy(allow)
	var temporary []int
	for i, r := range allow {
		// A capture-redundant allow can still be a review visibility exception.
		if in.Review.ProtectsAllow(r) {
			continue
		}
		f := Finding{Index: i, Rule: &allow[i], Admits: admits[i]}
		if len(f.Admits) > maxAdmitsPerRule {
			f.Admits = f.Admits[:maxAdmitsPerRule]
		}
		folder, folderOnly := cwdOnly(r)
		switch {
		case hasKey(covered, i):
			f.Code, f.Level, f.Fix, f.By, f.Admits = CodeCovered, LevelTidy, FixSafe, &allow[covered[i]], nil
			f.Message = "Another rule covers it, so it adds nothing."
		case shadowedBy(deny, r) != nil:
			f.Code, f.Level, f.Fix, f.By, f.Admits = CodeShadowed, LevelTidy, FixSafe, shadowedBy(deny, r), nil
			f.Message = "A deny rule matches every project it matches, so it admits nothing."
		case folderOnly && homeFolder(folder):
			f.Code, f.Level, f.Fix = CodeHomeFolder, LevelProblem, FixNarrow
			f.Message = "It names a home directory or a folder above one, so it admits every project anyone starts there, on every device."
			f.Replace = narrowed(allow, i, admits[i])
		case folderOnly && strings.Count(folder, "/") == 1:
			f.Code, f.Level, f.Fix = CodeBroadFolder, LevelWarning, FixNarrow
			f.Message = "It names a top-level folder, so it admits everything under it."
			f.Replace = narrowed(allow, i, admits[i])
		case r == (config.ProjectMatch{GitRemotePrefix: r.GitRemotePrefix}) && !strings.Contains(config.NormalizeRemote(r.GitRemotePrefix), "/"):
			f.Code, f.Level, f.Fix = CodeHostWide, LevelWarning, FixNarrow
			f.Message = "It names a whole host, so it admits every repository there, including other people's."
			f.Replace = narrowed(allow, i, admits[i])
		case folderOnly && Temporary(folder):
			f.Code, f.Level = CodeTemporary, LevelTidy
			if len(admits[i]) == 0 && len(rep.Unlisted) == 0 {
				f.Fix = FixSafe
				f.Message = "It names a temporary folder no device holds sessions for, and no new session will start there."
			} else {
				f.Fix = FixNarrow
				f.Message = "It names a temporary folder. No new session will start there; removing it and hiding what it admitted keeps the review queue clear."
				temporary = append(temporary, i)
			}
		case len(admits[i]) == 0 && len(in.Devices) > 0:
			f.Code, f.Level, f.Fix = CodeUnmatched, LevelInfo, FixRemove
			f.Message = "No project on a listed device matches it. It may be for a project nobody has opened yet."
		default:
			continue
		}
		rep.Findings = append(rep.Findings, f)
	}
	hideTemporary(in, &rep, temporary, admits)
	for _, o := range OwnerFolds(allow) {
		rep.Findings = append(rep.Findings, Finding{Code: CodeOwnerFold, Level: LevelOffer, Fix: FixWiden, Index: -1, Group: o.Owner, Count: o.Count,
			Message: fmt.Sprintf("%d git_remote rules name repositories under %s. One git_remote_prefix would cover them, and any other repository there.", o.Count, o.Owner)})
	}
	for _, o := range FolderFolds(allow) {
		// Folding an explicit review exception into a broader capture rule
		// could hide it. Leave these rules for the operator to edit.
		if slices.ContainsFunc(allow, func(r config.ProjectMatch) bool {
			return in.Review.ProtectsAllow(r) && config.Covers(config.ProjectMatch{CWDPrefix: o.Folder}, r)
		}) {
			continue
		}
		rep.Findings = append(rep.Findings, Finding{Code: CodeFolderFold, Level: LevelOffer, Fix: FixWiden, Index: -1, Group: o.Folder, Count: o.Count,
			Message: fmt.Sprintf("%d cwd_prefix rules name folders under %s. One cwd_prefix would cover them, and anything else there.", o.Count, o.Folder)})
	}
	groups := pending(in, &rep)
	if len(allow) >= RuleCap || len(deny) >= RuleCap {
		rep.Findings = append(rep.Findings, Finding{Code: CodeAtCap, Level: LevelProblem, Fix: FixNone, Index: -1, Count: max(len(allow), len(deny)),
			Message: fmt.Sprintf("The profile holds %d allow and %d deny rules, and a list holds at most %d. No Allow can add a rule until some go.", len(allow), len(deny), RuleCap)})
	} else if n := len(allow) + rep.Pending; max(len(allow), len(deny))*100 >= RuleCap*nearCapPercent || n > RuleCap {
		rep.Findings = append(rep.Findings, Finding{Code: CodeNearCap, Level: LevelWarning, Fix: FixNone, Index: -1, Count: len(allow),
			Message: fmt.Sprintf("The profile holds %d of %d allow rules, and %d projects wait in review. Allowing each with its own rule would add %d.", len(allow), RuleCap, rep.Pending, rep.Pending)})
	}
	rep.Findings = append(rep.Findings, groups...)
	if len(rep.Unlisted) > 0 {
		rep.Findings = append(rep.Findings, Finding{Code: CodeUnlisted, Level: LevelInfo, Fix: FixNone, Index: -1, Count: len(rep.Unlisted),
			Message: "These devices sent no inventory, so the doctor cannot say what the rules match there: " + strings.Join(rep.Unlisted, ", ") + "."})
	}
	rank := map[string]int{LevelProblem: 0, LevelWarning: 1, LevelTidy: 2, LevelOffer: 3, LevelInfo: 4}
	slices.SortStableFunc(rep.Findings, func(a, b Finding) int { return cmp.Compare(rank[a.Level], rank[b.Level]) })
	return rep
}

func hasKey(m map[int]int, k int) bool { _, ok := m[k]; return ok }

// shadowedBy is a deny rule that covers r, or nil. Deny reads a doubt as
// a match, so a deny rule that covers r as an allow rule would refuses
// every project r matches.
func shadowedBy(deny []config.ProjectMatch, r config.ProjectMatch) *config.ProjectMatch {
	for i := range deny {
		if config.Covers(deny[i], r) {
			return &deny[i]
		}
	}
	return nil
}

// homeFolder reports whether a clean absolute folder is the root, a
// folder that holds home directories, or a home directory, root's
// included.
func homeFolder(dir string) bool {
	if dir == "/" || slices.Contains(rootHomes, dir) {
		return true
	}
	segs := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	return homes(segs[len(segs)-1]) || len(segs) >= 2 && homes(segs[len(segs)-2])
}

// tempRoots are folders whose contents the system or a test clears.
var tempRoots = []string{"/tmp", "/var/tmp", "/private/tmp", "/dev/shm", "/var/folders", "/private/var/folders"}

// tempSegment matches a folder name a temp-folder maker gives: mktemp's
// tmp.XXXXXXXXXX, and Go's t.TempDir, TestName1234567/001.
var tempSegment = regexp.MustCompile(`^(tmp\.[A-Za-z0-9]{6,}|Test[A-Za-z0-9_]*[0-9]{6,})$`)

// Temporary reports whether a clean absolute folder looks temporary:
// under a temp root, under a .cache folder, or named as mktemp and Go
// tests name folders. A temp root itself is not: a rule for /tmp is a
// broad rule, not a stale one.
func Temporary(dir string) bool {
	for _, root := range tempRoots {
		if strings.HasPrefix(dir, root+"/") {
			return true
		}
	}
	for _, seg := range strings.Split(dir, "/") {
		if seg == ".cache" || tempSegment.MatchString(seg) {
			return true
		}
	}
	return false
}

// TemporaryDeny is the deny rule that keeps new sessions under a
// temporary folder dir out of capture and review: a cwd_prefix for the
// temp root dir is under, or for the .cache folder that holds it. A
// folder that is temporary only by its name, as mktemp and Go tests
// name them, has no such root and gets none. See
// TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2.
func TemporaryDeny(dir string) (config.ProjectMatch, bool) {
	dir = path.Clean(dir)
	for _, root := range tempRoots {
		if dir == root || strings.HasPrefix(dir, root+"/") {
			return config.ProjectMatch{CWDPrefix: root}, true
		}
	}
	if i := strings.Index(dir+"/", "/.cache/"); i > 0 {
		return config.ProjectMatch{CWDPrefix: dir[:i+len("/.cache")]}, true
	}
	return config.ProjectMatch{}, false
}

// admitted maps each allow rule's index to the projects it admits on the
// listed devices: projects it matches that no deny rule refuses.
func admitted(in Input) map[int][]Project {
	type at struct {
		i int
		k catalog.ProjectKey
	}
	seen := map[at]*Project{}
	out := map[int][]Project{}
	var order []at
	for _, d := range in.Devices {
		for _, p := range d.Projects {
			k, ok := catalog.ProjectKeyOf(p)
			if !ok {
				continue
			}
			id := config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote}
			for i, r := range in.Projects.Allow {
				if !(config.Projects{Allow: []config.ProjectMatch{r}, Deny: in.Projects.Deny}).Permitted(id) {
					continue
				}
				a := at{i, k}
				pr := seen[a]
				if pr == nil {
					pr = &Project{Key: k, rule: exactRule(p)}
					seen[a] = pr
					order = append(order, a)
				}
				if !slices.Contains(pr.Devices, d.Name) {
					pr.Devices = append(pr.Devices, d.Name)
				}
				pr.Sessions += p.Sessions
				if p.Newest.After(pr.Newest) {
					pr.Newest = p.Newest
				}
			}
		}
	}
	for _, a := range order {
		out[a.i] = append(out[a.i], *seen[a])
	}
	for i := range out {
		slices.SortFunc(out[i], func(a, b Project) int {
			return cmp.Or(cmp.Compare(b.Sessions, a.Sessions), cmp.Compare(a.Key.Key, b.Key.Key))
		})
	}
	return out
}

// exactRule is the narrowest rule that still names p on every device:
// its remote, or its folder. A folder that is a home directory or
// top-level gets its cwd_hash, which names that folder alone and nothing
// under it.
func exactRule(p protocol.InventoryProject) config.ProjectMatch {
	if r := config.NormalizeRemote(p.GitRemote); r != "" {
		return config.ProjectMatch{GitRemote: r}
	}
	c := path.Clean(strings.TrimSpace(p.CWD))
	if (homeFolder(c) || strings.Count(c, "/") == 1) && p.CWDHash != "" {
		return config.ProjectMatch{CWDHash: strings.ToLower(p.CWDHash)}
	}
	return config.ProjectMatch{CWDPrefix: c}
}

// narrowed is the exact rules that keep what allow[i] admits today,
// less those another rule of allow already covers. Each is within
// allow[i], so the fix never admits what the rule refused: a repository
// admitted under a folder keeps the folder, and is not admitted in
// every checkout.
func narrowed(allow []config.ProjectMatch, i int, admits []Project) []config.ProjectMatch {
	others := slices.Delete(slices.Clone(allow), i, i+1)
	out := []config.ProjectMatch{}
	for _, p := range admits {
		r := within(allow[i], p.rule)
		if !coveredByAny(others, r) && !coveredByAny(out, r) {
			out = append(out, r)
		}
	}
	return out
}

// within is exact narrowed to wide: exact with each field of wide that
// exact sets no narrower field for. exact names a project wide matches,
// so its cwd_prefix lies under wide's and its git_remote under wide's
// git_remote_prefix.
func within(wide, exact config.ProjectMatch) config.ProjectMatch {
	if exact.CWDPrefix == "" {
		exact.CWDPrefix = wide.CWDPrefix
	}
	if exact.GitRemote == "" && exact.GitRemotePrefix == "" {
		exact.GitRemote, exact.GitRemotePrefix = wide.GitRemote, wide.GitRemotePrefix
	}
	if exact.CWDHash == "" {
		exact.CWDHash = wide.CWDHash
	}
	if exact.CWDGlob == "" {
		exact.CWDGlob = wide.CWDGlob
	}
	return exact
}

// hideTemporary fills in the projects removing the temporary rules
// would stop admitting, which their fix hides from review. It reads
// admits whole: a finding's Admits is cut short for the page.
func hideTemporary(in Input, rep *Report, temporary []int, admits map[int][]Project) {
	if len(temporary) == 0 {
		return
	}
	after := in.Projects
	after.Allow = slices.Clone(after.Allow)
	for _, i := range slices.Backward(temporary) {
		after.Allow = slices.Delete(after.Allow, i, i+1)
	}
	dropped := map[catalog.ProjectKey]bool{}
	for _, d := range in.Devices {
		for _, p := range d.Projects {
			id := config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote}
			if k, ok := catalog.ProjectKeyOf(p); ok && in.Projects.Permitted(id) && !after.Permitted(id) {
				dropped[k] = true
			}
		}
	}
	for fi := range rep.Findings {
		f := &rep.Findings[fi]
		if f.Code != CodeTemporary || f.Fix != FixNarrow {
			continue
		}
		f.Replace = []config.ProjectMatch{}
		for _, p := range admits[f.Index] {
			if dropped[p.Key] && !in.Hidden[p.Key] && !slices.Contains(f.Hide, p.Key) {
				f.Hide = append(f.Hide, p.Key)
			}
		}
	}
}

// pending counts the refused projects waiting in review and groups the
// ones one owner or parent folder holds, which Allow at owner width
// takes as one rule.
func pending(in Input, rep *Report) []Finding {
	keys := map[catalog.ProjectKey]bool{}
	groups := map[config.ProjectMatch]int{}
	for _, d := range in.Devices {
		for _, p := range d.Projects {
			k, ok := catalog.ProjectKeyOf(p)
			if !ok || p.Allowed || keys[k] || in.Hidden[k] || p.Reason == config.RefusedByDeny || p.Reason == config.RefusedNoCWD {
				continue
			}
			if p.CWDs <= 1 {
				if _, hidden := in.Review.HiddenBy(config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote}, in.Projects.Allow); hidden {
					continue
				}
			}
			keys[k] = true
			if g, ok := GroupOf(p.GitRemote, p.CWD); ok {
				groups[g]++
			}
		}
	}
	rep.Pending = len(keys)
	var out []Finding
	for g, n := range groups {
		if n >= MinFold && !coveredByAny(in.Projects.Allow, g) {
			out = append(out, Finding{Code: CodeReviewGroup, Level: LevelOffer, Fix: FixNone, Index: -1, Group: g.GitRemotePrefix + g.CWDPrefix, Count: n,
				Message: fmt.Sprintf("%d projects waiting in review sit under %s. Allow them from the review queue with Rules for set to each repository's owner, or folder's parent, and they take one rule.", n, g.GitRemotePrefix+g.CWDPrefix)})
		}
	}
	slices.SortFunc(out, func(a, b Finding) int { return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Group, b.Group)) })
	if len(out) > maxGroupsShown {
		out = out[:maxGroupsShown]
	}
	return out
}

// Admits reports whether in's rules admit the project k on any device
// that holds it.
func Admits(in Input, k catalog.ProjectKey) bool {
	for _, d := range in.Devices {
		for _, p := range d.Projects {
			if pk, ok := catalog.ProjectKeyOf(p); ok && pk == k && in.Projects.Permitted(config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote}) {
				return true
			}
		}
	}
	return false
}

// ApplySafe removes every rule a safe finding names, and says how many
// went. The report must be the one Diagnose made for these rules.
func ApplySafe(p config.Projects, rep Report) (config.Projects, int) {
	var drop []int
	for _, f := range rep.Findings {
		if f.Fix == FixSafe && f.Index >= 0 && f.Index < len(p.Allow) {
			drop = append(drop, f.Index)
		}
	}
	return removeAt(p, drop), len(drop)
}

// ApplyUnmatched removes every rule that admits nothing today, safe or
// not, and says how many went.
func ApplyUnmatched(p config.Projects, rep Report) (config.Projects, int) {
	var drop []int
	for _, f := range rep.Findings {
		if (f.Fix == FixSafe || f.Fix == FixRemove) && f.Index >= 0 && f.Index < len(p.Allow) {
			drop = append(drop, f.Index)
		}
	}
	return removeAt(p, drop), len(drop)
}

// ApplyNarrow replaces the rule a narrowing finding names with its
// Replace rules, in its place, and returns the projects to hide from
// review. The finding must come from the report Diagnose made for p.
func ApplyNarrow(p config.Projects, f Finding) (config.Projects, []catalog.ProjectKey, bool) {
	if f.Fix != FixNarrow || f.Index < 0 || f.Index >= len(p.Allow) || f.Rule == nil || p.Allow[f.Index] != *f.Rule {
		return p, nil, false
	}
	allow := slices.Concat(p.Allow[:f.Index], f.Replace, p.Allow[f.Index+1:])
	p.Allow = allow
	return p, f.Hide, true
}

// ApplyFixes applies the fixes of findings fs together, each from the
// report Diagnose made for p: a safe or remove fix removes its rule, and
// a narrowing puts its Replace rules in the rule's place. It returns the
// projects to hide from review, each once, and how many fixes it
// applied. A finding that names no rule at its index, or a rule already
// fixed, is skipped. See TKT-01M6F4MD2QAGQNJ9D6XSYWY7J2.
func ApplyFixes(p config.Projects, fs []Finding) (config.Projects, []catalog.ProjectKey, int) {
	fs = slices.Clone(fs)
	// From the end of the list, so an earlier index still names its rule.
	slices.SortStableFunc(fs, func(a, b Finding) int { return cmp.Compare(b.Index, a.Index) })
	allow := slices.Clone(p.Allow)
	var hide []catalog.ProjectKey
	n, last := 0, -1
	for _, f := range fs {
		if f.Index < 0 || f.Index >= len(allow) || f.Index == last || f.Rule == nil || allow[f.Index] != *f.Rule {
			continue
		}
		switch f.Fix {
		case FixSafe, FixRemove:
			allow = slices.Delete(allow, f.Index, f.Index+1)
		case FixNarrow:
			allow = slices.Concat(allow[:f.Index], f.Replace, allow[f.Index+1:])
			for _, h := range f.Hide {
				if !slices.Contains(hide, h) {
					hide = append(hide, h)
				}
			}
		default:
			continue
		}
		n, last = n+1, f.Index
	}
	p.Allow = allow
	return p, hide, n
}

// removeAt is p less the allow rules at the indexes in drop.
func removeAt(p config.Projects, drop []int) config.Projects {
	gone := map[int]bool{}
	for _, i := range drop {
		gone[i] = true
	}
	var allow []config.ProjectMatch
	for i, r := range p.Allow {
		if !gone[i] {
			allow = append(allow, r)
		}
	}
	p.Allow = allow
	return p
}
