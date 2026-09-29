package web

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"terva.sh/lampi/internal/advisory"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/release"
	"terva.sh/lampi/internal/webauth"
)

// deviceRow is one device as the devices page shows it: the catalog's
// record, the newest report its agent sent, and how both compare with
// the lake.
type deviceRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Source      string `json:"source"`
	Profile     string `json:"profile"`
	MachineID   string `json:"machine_id,omitempty"`
	Created     string `json:"created"`
	LastContact string `json:"last_contact,omitempty"`
	LastData    string `json:"last_data,omitempty"`
	Freshness   string `json:"freshness"`
	// Reported is when the lake received the newest report; empty for a
	// device that has sent none, whose agent fields are unknown.
	Reported string `json:"reported,omitempty"`
	// AgentVersion is the release the agent reports. VersionState is
	// current, behind or ahead of the lake's release; unstamped for a
	// build that is not a release; unknown with no report, or when the
	// lake itself is not a release.
	AgentVersion string `json:"agent_version,omitempty"`
	VersionState string `json:"version_state"`
	Inventory    string `json:"inventory,omitempty"`
	// ProfileState is current when the agent applied the version the
	// lake would serve it now, stale when it applied another, unknown
	// when it has not said, and missing when the device names a profile
	// the lake no longer holds.
	AppliedVersion string `json:"applied_version,omitempty"`
	CurrentVersion string `json:"current_version,omitempty"`
	ProfileState   string `json:"profile_state"`
	// AllowSource is where the agent's allow rules come from. A local
	// source means the lake's profile does not decide what it uploads.
	AllowSource string `json:"allow_source,omitempty"`
	DenySource  string `json:"deny_source,omitempty"`
	// NoProfile is a token-file device whose agent reports no applied
	// profile: it has not pinned this lake, so no profile reaches it,
	// whatever the device is set to. terva-lampi lakes adopt on that
	// machine pins it. A registered agent always pins its lake.
	NoProfile   bool              `json:"no_profile,omitempty"`
	LastSync    *deviceSyncCounts `json:"last_sync,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
	LastErrorAt string            `json:"last_error_at,omitempty"`
	// Advisory is what this lake knows against the agent's release.
	Advisory *agentAdvisory `json:"advisory,omitempty"`
	last     time.Time
	contact  time.Time
}

// agentAdvisory is the advisory an agent's release matches.
type agentAdvisory struct {
	Severity advisory.Severity `json:"severity"`
	Reason   string            `json:"reason"`
	Link     string            `json:"link,omitempty"`
	// Fixed is the first release without the problem; empty when there
	// is none yet.
	Fixed string `json:"fixed,omitempty"`
}

// agentAdvisories is what the devices view matches against: the
// advisories this build ships, or a test's.
var agentAdvisories = advisory.Agents

type deviceSyncCounts struct {
	At          string `json:"at"`
	Uploaded    int    `json:"uploaded"`
	Manifests   int    `json:"manifests"`
	Refused     int    `json:"refused"`
	Quarantined int    `json:"quarantined"`
	Unchanged   int    `json:"unchanged"`
}

// devicesView is the devices page and GET /api/web/v1/devices.
type devicesView struct {
	AsOf        string      `json:"as_of"`
	LakeRelease string      `json:"lake_release,omitempty"`
	Devices     []deviceRow `json:"devices"`
	// Behind counts active devices whose agent is behind the lake, and
	// LocalRules those whose allow rules the lake's profile does not
	// set.
	Behind     int `json:"behind"`
	LocalRules int `json:"local_rules"`
	// Urgent names the active devices whose agent release matches an
	// urgent advisory.
	Urgent []string `json:"urgent"`
	// Actions offers the operator's forms, with the CSRF token they
	// carry and the profiles a device can be set to. Problem says why
	// the last action was refused. The page uses these; the API does not.
	Actions  bool     `json:"-"`
	CSRF     string   `json:"-"`
	Profiles []string `json:"-"`
	Problem  string   `json:"-"`
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readDevices(ctx, s.now())
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) devicesPage(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	s.renderDevices(w, r, "", http.StatusOK)
}

// renderDevices lists the devices, with the operator's forms for an
// operator, and problem when an action was refused.
func (s *Server) renderDevices(w http.ResponseWriter, r *http.Request, problem string, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readDevices(ctx, s.now())
	if err != nil {
		pageError(w, r, err)
		return
	}
	v.Problem = problem
	if id, csrf := webauth.Current(r); id.Operator && s.reg != nil {
		v.Actions, v.CSRF = true, csrf
		if v.Profiles, err = s.reg.Lake().Catalog.ProfileNames(ctx); err != nil {
			s.logError(r, "listing profiles failed", err)
			v.Profiles = []string{config.DefaultProfile}
		}
	}
	renderStatus(w, r, pageData{Title: "Devices", View: "devices", AsOf: v.AsOf, Devices: v, Urgent: v.Urgent}, status)
}

func (s *Server) readDevices(ctx context.Context, now time.Time) (devicesView, error) {
	now = now.UTC()
	v := devicesView{AsOf: now.Format(time.RFC3339Nano), Devices: []deviceRow{}, Urgent: []string{}}
	var lakeV release.Version
	var lakeKnown bool
	if s.ops != nil {
		if lakeV, lakeKnown = release.Parse(s.ops.Release); lakeKnown {
			v.LakeRelease = lakeV.String()
		}
	}
	devices, err := s.catalog.Devices(ctx)
	if err != nil {
		return v, err
	}
	reports, err := s.catalog.DeviceReports(ctx)
	if err != nil {
		return v, err
	}
	byDevice := make(map[string]catalog.DeviceReport, len(reports))
	for _, rep := range reports {
		byDevice[rep.DeviceID] = rep
	}
	activity, err := s.catalog.MachinesActivity(ctx, now.Add(-24*time.Hour))
	if err != nil {
		return v, err
	}
	byMachine := make(map[string]catalog.MachineActivity, len(activity))
	for _, a := range activity {
		byMachine[a.MachineID] = a
	}
	var contacts map[string]time.Time
	if s.ops != nil && s.ops.Contacts != nil {
		contacts = s.ops.Contacts()
	}
	for _, d := range devices {
		row := deviceRow{
			ID: d.ID, Name: d.Name, State: d.State(), Source: d.Source, Profile: d.Profile, MachineID: d.MachineID,
			Created: d.Created.UTC().Format(time.RFC3339), VersionState: "unknown", ProfileState: "unknown",
		}
		if row.Profile == "" {
			row.Profile = config.DefaultProfile
		}
		if t, ok := contacts[d.ID]; ok {
			row.LastContact = t.UTC().Format(time.RFC3339)
			row.last, row.contact = t, t
		}
		if a, ok := byMachine[d.MachineID]; ok && d.MachineID != "" {
			last := a.LastUpload
			if a.LastUpdate.After(last) {
				last = a.LastUpdate
			}
			if !last.IsZero() {
				row.LastData = last.UTC().Format(time.RFC3339)
				if last.After(row.last) {
					row.last = last
				}
			}
		}
		eff, err := s.catalog.ResolveProfile(ctx, d)
		switch {
		case errors.Is(err, catalog.ErrNoProfile):
			// The catalog refuses to delete a profile a device names, so
			// this is a catalog out of step. Its agent cannot fetch a
			// profile, whatever it reported; say so on its row rather
			// than failing the page.
			row.ProfileState = "missing"
		case err != nil:
			return v, err
		default:
			row.CurrentVersion = eff.Version
		}
		if rep, ok := byDevice[d.ID]; ok {
			addReport(&row, rep, lakeV, lakeKnown)
		}
		row.Freshness = freshness(row.last, now)
		if row.State == "active" {
			if row.VersionState == "behind" {
				v.Behind++
			}
			if row.AllowSource == config.OriginLocal {
				v.LocalRules++
			}
			if row.Advisory != nil && row.Advisory.Severity == advisory.Urgent {
				v.Urgent = append(v.Urgent, row.Name)
			}
		}
		v.Devices = append(v.Devices, row)
	}
	// Active devices first, then by how recently each was heard from.
	sort.SliceStable(v.Devices, func(i, j int) bool {
		ri, rj := v.Devices[i].State != "active", v.Devices[j].State != "active"
		if ri != rj {
			return rj
		}
		return v.Devices[i].last.After(v.Devices[j].last)
	})
	return v, nil
}

// addReport fills the row's agent fields from its newest report.
func addReport(row *deviceRow, rep catalog.DeviceReport, lakeV release.Version, lakeKnown bool) {
	r := rep.Report
	row.Reported = rep.Received.UTC().Format(time.RFC3339)
	// After a serve restart the live contacts start empty, so a report is
	// the newest word from the device until it makes another request.
	if rep.Received.After(row.last) {
		row.last = rep.Received
	}
	if row.LastContact == "" || rep.Received.After(row.contact) {
		row.LastContact = row.Reported
	}
	row.AgentVersion = r.AgentVersion
	row.Inventory = r.Inventory
	row.AllowSource, row.DenySource = r.AllowSource, r.DenySource
	row.NoProfile = row.Source == catalog.DeviceFromTokenFile && r.Profile == "" && r.ProfileVersion == ""
	row.AppliedVersion = r.ProfileVersion
	row.VersionState = versionState(r.AgentVersion, lakeV, lakeKnown)
	if a, ok := agentAdvisories.Match(r.AgentVersion); ok {
		row.Advisory = &agentAdvisory{Severity: a.Severity, Reason: a.Reason, Link: a.Link, Fixed: a.Fixed}
	}
	switch {
	case row.ProfileState == "missing", r.ProfileVersion == "":
	case r.ProfileVersion == row.CurrentVersion:
		row.ProfileState = "current"
	default:
		row.ProfileState = "stale"
	}
	if s := r.LastSync; s != nil {
		row.LastSync = &deviceSyncCounts{At: s.At.UTC().Format(time.RFC3339), Uploaded: s.Uploaded, Manifests: s.Manifests, Refused: s.Refused, Quarantined: s.Quarantined, Unchanged: s.Unchanged}
	}
	if r.LastError != "" {
		row.LastError = r.LastError
		row.LastErrorAt = r.LastErrorAt.UTC().Format(time.RFC3339)
	}
}

// versionState compares an agent's reported release with the lake's.
func versionState(agent string, lakeV release.Version, lakeKnown bool) string {
	if agent == "" {
		return "unknown"
	}
	v, ok := release.Parse(agent)
	switch {
	case !ok:
		return "unstamped"
	case !lakeKnown:
		return "unknown"
	}
	switch v.Compare(lakeV) {
	case -1:
		return "behind"
	case 1:
		return "ahead"
	}
	return "current"
}
