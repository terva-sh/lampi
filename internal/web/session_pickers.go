package web

import (
	"cmp"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"terva.sh/lampi/internal/catalog"
)

// The sessions filters' pickers (TKT-01M6F7JMQYGY3WGGV55H0M3FR5): the
// Project box suggests the remotes and folders the reader's sessions
// hold, with the owners and folders that group them, and the Machine
// select lists the machines that posted them, by device name. Both come
// from the catalog's project and machine indexes, cached for a minute
// per scope so that a page render does not scan.

const (
	pickerTTL = time.Minute
	// maxPickerOptions bounds the Project suggestions.
	maxPickerOptions = 500
)

type pickerOption struct{ Value, Label string }

type machineOption struct{ ID, Name string }

type sessionPickers struct {
	Projects []pickerOption
	Machines []machineOption
}

type pickerCache struct {
	mu sync.Mutex
	at map[string]pickerEntry
}

type pickerEntry struct {
	when time.Time
	v    sessionPickers
}

// sessionPickers is the filters' choices for the request's scope. A
// catalog error leaves them empty: the filters still take typed values.
func (s *Server) sessionPickers(r *http.Request) sessionPickers {
	return s.sessionPickersIn(r, scopeOf(r))
}

// sessionPickersIn is sessionPickers for scope, for a page whose guard
// sets none, such as an admin's.
func (s *Server) sessionPickersIn(r *http.Request, scope catalog.Scope) sessionPickers {
	key := fmt.Sprintf("%#v", scope)
	now := s.now()
	s.pickers.mu.Lock()
	if e, ok := s.pickers.at[key]; ok && now.Sub(e.when) < pickerTTL {
		s.pickers.mu.Unlock()
		return e.v
	}
	s.pickers.mu.Unlock()
	projects, machines, err := s.catalog.ProjectChoices(r.Context(), scope)
	if err != nil {
		s.logError(r, "listing the sessions filters' choices failed", err)
		return sessionPickers{}
	}
	v := sessionPickers{Projects: projectOptions(projects)}
	names := s.machineNames(r.Context())
	for _, m := range machines {
		name := names[m]
		if name == "" {
			name = m
		}
		v.Machines = append(v.Machines, machineOption{ID: m, Name: name})
	}
	slices.SortFunc(v.Machines, func(a, b machineOption) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID)) })
	s.pickers.mu.Lock()
	if s.pickers.at == nil {
		s.pickers.at = map[string]pickerEntry{}
	}
	s.pickers.at[key] = pickerEntry{when: now, v: v}
	s.pickers.mu.Unlock()
	return v
}

// projectOptions is the Project box's suggestions: first the owners that
// hold two or more repositories and the folders where two or more
// project folders branch, then each repository and folder, each group
// by sessions, most first.
func projectOptions(cs []catalog.ProjectChoice) []pickerOption {
	type agg struct {
		sessions int
		members  map[string]bool
		children map[string]bool
	}
	groups := map[string]*agg{}
	get := func(k string) *agg {
		a := groups[k]
		if a == nil {
			a = &agg{members: map[string]bool{}, children: map[string]bool{}}
			groups[k] = a
		}
		return a
	}
	type single struct {
		value, kind string
		sessions    int
	}
	var singles []single
	for _, c := range cs {
		switch {
		case c.Remote != "":
			singles = append(singles, single{c.Remote, "repository", c.Sessions})
			if owner := path.Dir(c.Remote); owner != "." && strings.Contains(owner, "/") {
				a := get("remote " + owner)
				a.sessions += c.Sessions
				a.members[c.Remote] = true
			}
		case c.Folder != "":
			singles = append(singles, single{c.Folder, "folder", c.Sessions})
			child := c.Folder
			for dir := path.Dir(child); dir != "/" && dir != "."; child, dir = dir, path.Dir(dir) {
				a := get("folder " + dir)
				a.sessions += c.Sessions
				a.members[c.Folder] = true
				a.children[child] = true
			}
		}
	}
	type group struct {
		value, label string
		sessions     int
	}
	var gs []group
	for k, a := range groups {
		kind, value, _ := strings.Cut(k, " ")
		switch {
		case kind == "remote" && len(a.members) >= 2:
			gs = append(gs, group{value, "owner · " + strconv.Itoa(len(a.members)) + " repositories · " + sessionsText(a.sessions), a.sessions})
		case kind == "folder" && len(a.children) >= 2:
			gs = append(gs, group{value, "folder · " + strconv.Itoa(len(a.members)) + " project folders · " + sessionsText(a.sessions), a.sessions})
		}
	}
	slices.SortFunc(gs, func(a, b group) int {
		return cmp.Or(cmp.Compare(b.sessions, a.sessions), cmp.Compare(a.value, b.value))
	})
	slices.SortFunc(singles, func(a, b single) int {
		return cmp.Or(cmp.Compare(b.sessions, a.sessions), cmp.Compare(a.value, b.value))
	})
	var out []pickerOption
	for _, g := range gs {
		out = append(out, pickerOption{g.value, g.label})
	}
	for _, s := range singles {
		out = append(out, pickerOption{s.value, s.kind + " · " + sessionsText(s.sessions)})
	}
	if len(out) > maxPickerOptions {
		out = out[:maxPickerOptions]
	}
	return out
}

func sessionsText(n int) string { return strconv.Itoa(n) + " " + plural(n, "session", "sessions") }
