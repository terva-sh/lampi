package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/webauth"
)

// IgnoredProfiles is a profiles file serve does not read, and the
// command that imports it into the catalog.
type IgnoredProfiles struct {
	Path   string `json:"path"`
	Import string `json:"import"`
}

// profileHarnessIDs are the harnesses a profile can turn on or off, in
// the order the page lists them.
var profileHarnessIDs = []string{
	protocol.HarnessClaude, protocol.HarnessCodex, protocol.HarnessOpenCode,
	protocol.HarnessCursor, protocol.HarnessCursorCLI, protocol.HarnessTerva,
}

// maxProfileRevisions is how many revisions the profile page lists,
// newest first.
const maxProfileRevisions = 50

type profileSummary struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Revision int64  `json:"revision,omitempty"`
	// Stored is false for a default profile no one has saved, which
	// the lake serves empty.
	Stored    bool   `json:"stored"`
	Updated   string `json:"updated,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
	// Devices counts the active devices that fetch this profile.
	Devices int `json:"devices"`
}

type profilesView struct {
	AsOf     string            `json:"as_of"`
	Profiles []profileSummary  `json:"profiles"`
	Ignored  []IgnoredProfiles `json:"ignored_files"`
	// Actions offers the operator's forms; the page uses these, the API
	// does not.
	Actions bool   `json:"-"`
	CSRF    string `json:"-"`
	Problem string `json:"-"`
}

// profileHarness is one harness toggle: set is false when the profile
// leaves the harness as the agent has it.
type profileHarness struct {
	ID      string `json:"id"`
	Set     bool   `json:"set"`
	Enabled bool   `json:"enabled"`
}

type profileDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// AllowSource is where the agent says its allow rules come from;
	// local means this profile's allow rules do not reach it.
	AllowSource string `json:"allow_source,omitempty"`
}

type profileRevisionView struct {
	ID        int64  `json:"id"`
	Version   string `json:"version,omitempty"`
	Note      string `json:"note,omitempty"`
	Deleted   bool   `json:"deleted"`
	Created   string `json:"created"`
	CreatedBy string `json:"created_by"`
}

type profileView struct {
	profileSummary
	AsOf     string          `json:"as_of"`
	Document json.RawMessage `json:"document"`
	// The document's fields as the page lays them out.
	Allow       []config.ProjectMatch `json:"-"`
	Deny        []config.ProjectMatch `json:"-"`
	Harnesses   []profileHarness      `json:"harnesses"`
	Debounce    string                `json:"-"`
	DebounceMax string                `json:"-"`
	// DeviceList is the active devices that fetch this profile.
	DeviceList []profileDevice       `json:"device_list"`
	Revisions  []profileRevisionView `json:"revisions"`
	Ignored    []IgnoredProfiles     `json:"ignored_files"`
	Actions    bool                  `json:"-"`
	CSRF       string                `json:"-"`
}

func (s *Server) ignoredProfiles() []IgnoredProfiles {
	if s.ops == nil || s.ops.IgnoredProfiles == nil {
		return []IgnoredProfiles{}
	}
	if v := s.ops.IgnoredProfiles(); v != nil {
		return v
	}
	return []IgnoredProfiles{}
}

// profileUsers maps each profile name to the active devices that fetch
// it, with the allow source each last reported.
func (s *Server) profileUsers(ctx context.Context) (map[string][]profileDevice, error) {
	devices, err := s.catalog.Devices(ctx)
	if err != nil {
		return nil, err
	}
	reports, err := s.catalog.DeviceReports(ctx)
	if err != nil {
		return nil, err
	}
	allow := make(map[string]string, len(reports))
	for _, r := range reports {
		allow[r.DeviceID] = r.Report.AllowSource
	}
	users := map[string][]profileDevice{}
	for _, d := range devices {
		if d.State() != "active" {
			continue
		}
		name := d.Profile
		if name == "" {
			name = config.DefaultProfile
		}
		users[name] = append(users[name], profileDevice{ID: d.ID, Name: d.Name, AllowSource: allow[d.ID]})
	}
	return users, nil
}

func summarize(p catalog.Profile, users int) profileSummary {
	return profileSummary{Name: p.Name, Version: p.Version, Revision: p.Revision, Stored: true,
		Updated: p.Updated.UTC().Format(time.RFC3339), UpdatedBy: p.UpdatedBy, Devices: users}
}

// emptyDefault is the default profile the lake serves when none is
// stored.
func emptyDefault(users int) profileSummary {
	return profileSummary{Name: config.DefaultProfile, Version: config.Profile{}.Version(), Devices: users}
}

func (s *Server) readProfiles(ctx context.Context, now time.Time) (profilesView, error) {
	v := profilesView{AsOf: now.UTC().Format(time.RFC3339Nano), Profiles: []profileSummary{}, Ignored: s.ignoredProfiles()}
	list, err := s.catalog.Profiles(ctx)
	if err != nil {
		return v, err
	}
	users, err := s.profileUsers(ctx)
	if err != nil {
		return v, err
	}
	haveDefault := false
	for _, p := range list {
		haveDefault = haveDefault || p.Name == config.DefaultProfile
		v.Profiles = append(v.Profiles, summarize(p, len(users[p.Name])))
	}
	if !haveDefault {
		v.Profiles = append(v.Profiles, emptyDefault(len(users[config.DefaultProfile])))
	}
	// The default first, then by name.
	sort.SliceStable(v.Profiles, func(i, j int) bool {
		a, b := v.Profiles[i].Name, v.Profiles[j].Name
		if (a == config.DefaultProfile) != (b == config.DefaultProfile) {
			return a == config.DefaultProfile
		}
		return a < b
	})
	return v, nil
}

func (s *Server) readProfile(ctx context.Context, name string, now time.Time) (profileView, error) {
	if !config.ValidProfileName(name) {
		return profileView{}, catalog.ErrNoProfile
	}
	users, err := s.profileUsers(ctx)
	if err != nil {
		return profileView{}, err
	}
	var prof config.Profile
	var sum profileSummary
	stored, err := s.catalog.ProfileByName(ctx, name)
	switch {
	case errors.Is(err, catalog.ErrNoProfile) && name == config.DefaultProfile:
		sum = emptyDefault(len(users[name]))
	case err != nil:
		return profileView{}, err
	default:
		prof, sum = stored.Config, summarize(stored, len(users[name]))
	}
	doc, err := json.Marshal(prof)
	if err != nil {
		return profileView{}, err
	}
	v := profileView{
		profileSummary: sum, AsOf: now.UTC().Format(time.RFC3339Nano), Document: doc,
		Allow: prof.Projects.Allow, Deny: prof.Projects.Deny,
		Debounce: prof.Agent.Debounce, DebounceMax: prof.Agent.DebounceMax,
		DeviceList: users[name], Revisions: []profileRevisionView{}, Ignored: s.ignoredProfiles(),
	}
	if v.DeviceList == nil {
		v.DeviceList = []profileDevice{}
	}
	for _, id := range profileHarnessIDs {
		h, set := prof.Harnesses[id]
		v.Harnesses = append(v.Harnesses, profileHarness{ID: id, Set: set, Enabled: h.Enabled})
	}
	revs, err := s.catalog.RecentProfileRevisions(ctx, name, maxProfileRevisions)
	if err != nil {
		return profileView{}, err
	}
	for _, r := range revs {
		v.Revisions = append(v.Revisions, profileRevisionView{ID: r.ID, Version: r.Version, Note: r.Note, Deleted: r.Deleted,
			Created: r.Created.UTC().Format(time.RFC3339), CreatedBy: r.CreatedBy})
	}
	return v, nil
}

func (s *Server) profiles(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readProfiles(ctx, s.now())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	name := r.PathValue("name")
	v, err := s.readProfile(ctx, name, s.now())
	if errors.Is(err, catalog.ErrNoProfile) {
		// latest_revision is the base_revision a PUT that creates it names.
		var latest int64
		if config.ValidProfileName(name) {
			if latest, err = s.catalog.LatestProfileRevision(ctx, name); err != nil {
				fail(w, r, err)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "not_found", "latest_revision": latest})
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) profilesPage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	s.renderProfiles(w, r, "", http.StatusOK)
}

// operatorForms reports whether the signed-in user gets the editor's
// forms, and the CSRF token they carry.
func (s *Server) operatorForms(r *http.Request) (bool, string) {
	id, csrf := webauth.Current(r)
	return id.Operator && s.reg != nil, csrf
}

func (s *Server) renderProfiles(w http.ResponseWriter, r *http.Request, problem string, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readProfiles(ctx, s.now())
	if err != nil {
		pageError(w, r, err)
		return
	}
	v.Problem = problem
	v.Actions, v.CSRF = s.operatorForms(r)
	renderStatus(w, r, pageData{Title: "Profiles", View: "profiles", AsOf: v.AsOf, Profiles: v}, status)
}

func (s *Server) profilePage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readProfile(ctx, r.PathValue("name"), s.now())
	if errors.Is(err, catalog.ErrNoProfile) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		pageError(w, r, err)
		return
	}
	v.Actions, v.CSRF = s.operatorForms(r)
	render(w, r, pageData{Title: "Profile " + v.Name, View: "profile", AsOf: v.AsOf, Profile: v})
}

func profileURL(name string) string { return "/profiles/" + url.PathEscape(name) }
