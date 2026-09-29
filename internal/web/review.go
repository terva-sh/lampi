package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

// The review queue, /review: every project the devices hold that no one
// has decided about yet, across all devices, so onboarding a machine is
// one page rather than a visit to each device. A project needs review
// while it is refused, allowable, not hidden, and not yet covered by an
// allow rule in the profile of the device holding it. See
// TKT-01M3N8F354.

const reviewPath = "/review"

// Review states of one device's copy of a project.
const (
	stateNeeds   = "needs_review"
	statePending = "allow_pending"
	stateDenied  = "denied"
)

// reviewFilter narrows the queue. Every field is optional.
type reviewFilter struct {
	Device  string `json:"device,omitempty"`
	Harness string `json:"harness,omitempty"`
	Profile string `json:"profile,omitempty"`
	// Hidden shows the Hidden tab. The API always carries every section.
	Hidden bool `json:"-"`
}

// query is the filter as a query string, "" when it has none.
func (f reviewFilter) query() string {
	q := url.Values{}
	for k, v := range map[string]string{"device": f.Device, "harness": f.Harness, "profile": f.Profile} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.Hidden {
		q.Set("tab", "hidden")
	}
	return q.Encode()
}

// URL is the review page with this filter.
func (f reviewFilter) URL() string {
	if q := f.query(); q != "" {
		return reviewPath + "?" + q
	}
	return reviewPath
}

// Any reports whether the filter narrows the queue.
func (f reviewFilter) Any() bool { return f.Device != "" || f.Harness != "" || f.Profile != "" }

var reviewHarnesses = []string{"terva", "claude", "codex", "opencode", "cursor", "cursor-cli"}

// parseReviewFilter reads the filter; ok is false for a key it does not
// know, a key given twice, or a value no filter takes. Empty values are
// what a submitted form sends for "all", and are ignored.
func parseReviewFilter(q url.Values) (reviewFilter, bool) {
	var f reviewFilter
	for k, v := range q {
		if len(v) != 1 {
			return f, false
		}
		switch val := v[0]; k {
		case "device":
			if val != "" && !validDeviceID(val) {
				return f, false
			}
			f.Device = val
		case "harness":
			if val != "" && !slices.Contains(reviewHarnesses, val) {
				return f, false
			}
			f.Harness = val
		case "profile":
			if val != "" && !config.ValidProfileName(val) {
				return f, false
			}
			f.Profile = val
		case "tab":
			if val != "" && val != "hidden" {
				return f, false
			}
			f.Hidden = val == "hidden"
		default:
			return f, false
		}
	}
	return f, true
}

// reviewSighting is one device's copy of a project, with its state.
type reviewSighting struct {
	catalog.ReviewSighting
	// ProfileName is the profile the device fetches, the default named.
	ProfileName string `json:"profile"`
	State       string `json:"state"`
	// LocalAllow is a device whose config.json sets its allow rules, so
	// a rule in its profile does not reach it.
	LocalAllow bool `json:"local_allow,omitempty"`
	// StillRefused is a copy whose profile allows it, from a device
	// that applied that profile and refused the project in an inventory
	// it sent since: waiting will not change it.
	StillRefused bool `json:"still_refused,omitempty"`
}

// reviewRow is one project, with the device copies the filter keeps.
type reviewRow struct {
	Key       catalog.ProjectKey `json:"key"`
	Sightings []reviewSighting   `json:"devices"`
	Sessions  int                `json:"sessions"`
	Bytes     int64              `json:"bytes"`
	Newest    time.Time          `json:"newest,omitzero"`
	FirstSeen time.Time          `json:"first_seen"`
	// Seeded is a project first seen when the lake began recording
	// sightings: it may have been on the device before.
	Seeded bool `json:"first_seen_at_or_before,omitempty"`
}

// hiddenRow is one hidden project and how many active devices refuse it
// now.
type hiddenRow struct {
	catalog.ProjectHide
	Devices int `json:"devices"`
}

// reviewView is the review page and GET /api/web/v1/review.
type reviewView struct {
	AsOf           string                   `json:"as_of"`
	SightingsSince string                   `json:"sightings_since,omitempty"`
	Filter         reviewFilter             `json:"filter"`
	Needs          []reviewRow              `json:"needs_review"`
	Pending        []reviewRow              `json:"allow_pending"`
	Denied         []reviewRow              `json:"denied"`
	Hidden         []hiddenRow              `json:"hidden"`
	Strict         []catalog.StrictRefusals `json:"strict"`
	// The page's forms and filter choices.
	Actions  bool        `json:"-"`
	CSRF     string      `json:"-"`
	Devices  []deviceRow `json:"-"`
	Profiles []string    `json:"-"`
	Notice   string      `json:"-"`
	Problem  string      `json:"-"`
	// Here is this page, for forms that come back to it.
	Here    string `json:"-"`
	sinceAt time.Time
}

// reviewTableView is one section's table: its rows, and whether they
// offer Allow.
type reviewTableView struct {
	View    reviewView
	Rows    []reviewRow
	Caption string
	Allow   bool
}

func isNoProfile(err error) bool { return errors.Is(err, catalog.ErrNoProfile) }

// readReview builds the queue, narrowed by f.
func (s *Server) readReview(ctx context.Context, f reviewFilter, now time.Time) (reviewView, error) {
	v := reviewView{AsOf: now.UTC().Format(time.RFC3339Nano), Filter: f, Needs: []reviewRow{}, Pending: []reviewRow{}, Denied: []reviewRow{}, Hidden: []hiddenRow{}, Strict: []catalog.StrictRefusals{}}
	q, err := s.catalog.ReviewQueue(ctx)
	if err != nil {
		return v, err
	}
	if !q.SightingsSince.IsZero() {
		v.sinceAt = q.SightingsSince
		v.SightingsSince = q.SightingsSince.UTC().Format(time.RFC3339)
	}
	dv, err := s.readDevices(ctx, now)
	if err != nil {
		return v, err
	}
	rows := map[string]deviceRow{}
	for _, d := range dv.Devices {
		rows[d.ID] = d
		if d.State == "active" {
			v.Devices = append(v.Devices, d)
		}
	}
	// Each device's effective profile, resolved once.
	effective := map[string]config.Projects{}
	projectsOf := func(sg catalog.ReviewSighting) (config.Projects, error) {
		if p, ok := effective[sg.DeviceID]; ok {
			return p, nil
		}
		e, err := s.catalog.ResolveProfile(ctx, catalog.Device{ID: sg.DeviceID, Profile: sg.Profile})
		if err != nil && !isNoProfile(err) {
			return config.Projects{}, err
		}
		// A device set to a profile the lake no longer holds gets
		// nothing from it: every project still needs a decision.
		effective[sg.DeviceID] = e.Config.Projects
		return e.Config.Projects, nil
	}
	// caughtUp reports whether device id has applied its current
	// profile and sent an inventory after both that profile's save and
	// the report saying it applied it: a refusal in that inventory is
	// not waiting on the profile any more. Without that order the lake
	// cannot tell, and the copy stays allow pending.
	saved := map[string]time.Time{}
	received := map[string]time.Time{}
	caughtUp := func(id, profile string) (bool, error) {
		if rows[id].ProfileState != "current" {
			return false, nil
		}
		at, ok := saved[profile]
		if !ok {
			p, err := s.catalog.ProfileByName(ctx, profile)
			if err != nil && !isNoProfile(err) {
				return false, err
			}
			at, saved[profile] = p.Updated, p.Updated
		}
		got, ok := received[id]
		if !ok {
			inv, _, err := s.catalog.DeviceInventoryOf(ctx, id)
			if err != nil {
				return false, err
			}
			got, received[id] = inv.Received, inv.Received
		}
		reported, err := time.Parse(time.RFC3339Nano, rows[id].Reported)
		if err != nil {
			return false, nil
		}
		// Reported is cut to the second; an inventory in the same second
		// may have come first.
		return got.After(at) && got.After(reported.Add(time.Second)), nil
	}
	seen := map[catalog.ProjectKey]int{}
	for _, p := range q.Projects {
		byState := map[string]*reviewRow{}
		for _, sg := range p.Sightings {
			name := sg.Profile
			if name == "" {
				name = config.DefaultProfile
			}
			if f.Device != "" && sg.DeviceID != f.Device || f.Profile != "" && name != f.Profile ||
				f.Harness != "" && !slices.Contains(sg.Project.Harnesses, f.Harness) {
				continue
			}
			seen[p.Key]++
			if p.Hidden != nil {
				continue
			}
			rs := reviewSighting{ReviewSighting: sg, ProfileName: name, LocalAllow: rows[sg.DeviceID].AllowSource == config.OriginLocal}
			projects, err := projectsOf(sg)
			if err != nil {
				return v, err
			}
			id := config.ProjectID{CWD: sg.Project.CWD, CWDHash: sg.Project.CWDHash, GitRemote: sg.Project.GitRemote}
			switch {
			// A profile's deny rules are added to the device's own, whatever
			// its deny source says (config.ApplyLakeProfile), so a
			// profile deny is always the device's decision.
			case !allowable(sg.Project.Reason), projects.Refusal(id) == config.RefusedByDeny:
				rs.State = stateDenied
			case !rs.LocalAllow && projects.Permitted(id):
				// A device whose config.json sets its allow rules takes
				// none from its profile, so a profile rule is no
				// decision for it: its copy still needs one. A device
				// that applied the profile and refused the project
				// since is not waiting on anything either.
				rs.State = statePending
				done, err := caughtUp(sg.DeviceID, name)
				if err != nil {
					return v, err
				}
				if done {
					rs.State, rs.StillRefused = stateNeeds, true
				}
			default:
				rs.State = stateNeeds
			}
			r := byState[rs.State]
			if r == nil {
				r = &reviewRow{Key: p.Key}
				byState[rs.State] = r
			}
			r.Sightings = append(r.Sightings, rs)
			r.Sessions += sg.Project.Sessions
			r.Bytes += sg.Project.Bytes
			if sg.Project.Newest.After(r.Newest) {
				r.Newest = sg.Project.Newest
			}
			if r.FirstSeen.IsZero() || sg.FirstSeen.Before(r.FirstSeen) {
				r.FirstSeen = sg.FirstSeen
			}
		}
		for state, list := range map[string]*[]reviewRow{stateNeeds: &v.Needs, statePending: &v.Pending, stateDenied: &v.Denied} {
			if r := byState[state]; r != nil {
				r.Seeded = !v.sinceAt.IsZero() && !r.FirstSeen.After(v.sinceAt)
				*list = append(*list, *r)
			}
		}
	}
	// Filtering can move a project's earliest sighting; keep newest
	// first within each section.
	for _, list := range [][]reviewRow{v.Needs, v.Pending, v.Denied} {
		slices.SortStableFunc(list, func(a, b reviewRow) int { return b.FirstSeen.Compare(a.FirstSeen) })
	}
	hides, err := s.catalog.HiddenProjects(ctx)
	if err != nil {
		return v, err
	}
	for _, h := range hides {
		if f.Any() && seen[h.Key] == 0 {
			continue
		}
		v.Hidden = append(v.Hidden, hiddenRow{ProjectHide: h, Devices: seen[h.Key]})
	}
	for _, st := range q.Strict {
		name := rows[st.DeviceID].Profile
		if f.Device != "" && st.DeviceID != f.Device || f.Profile != "" && name != f.Profile || f.Harness != "" {
			continue
		}
		v.Strict = append(v.Strict, st)
	}
	return v, nil
}

// reviewCount is how many projects need review, for the header; -1
// when it cannot be read, so the header shows no count rather than a
// wrong one.
func (s *Server) reviewCount(r *http.Request) int {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readReview(ctx, reviewFilter{}, s.now())
	if err != nil {
		s.logError(r, "counting the review queue failed", err)
		return -1
	}
	return len(v.Needs)
}

func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	f, ok := parseReviewFilter(r.URL.Query())
	if !ok || f.Hidden {
		fail(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readReview(ctx, f, s.now())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) reviewPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	saved, rev, present := savedQuery(q)
	if present {
		q.Del("saved")
		q.Del("revision")
	}
	f, ok := parseReviewFilter(q)
	if !ok {
		pageError(w, r, catalog.ErrPage)
		return
	}
	s.renderReview(w, r, f, "", saved, rev, http.StatusOK)
}

// renderReview shows the queue narrowed by f, with problem after a
// refused action, or a notice of what a save that came back here saved.
func (s *Server) renderReview(w http.ResponseWriter, r *http.Request, f reviewFilter, problem, saved string, rev int64, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readReview(ctx, f, s.now())
	if err != nil {
		pageError(w, r, err)
		return
	}
	v.Here, v.Problem = f.URL(), problem
	if saved != "" {
		v.Notice = s.savedNotice(r, saved, rev)
	}
	if ident, csrf := webauth.Current(r); ident.Operator && s.reg != nil {
		v.Actions, v.CSRF = true, csrf
	}
	if v.Profiles, err = s.catalog.ProfileNames(ctx); err != nil {
		s.logError(r, "listing profiles failed", err)
		v.Profiles = []string{config.DefaultProfile}
	}
	renderStatus(w, r, pageData{Title: "Review", View: "review", AsOf: v.AsOf, Review: v}, status)
}
