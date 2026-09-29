package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"terva.sh/lampi/internal/advisory"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/webauth"
)

// deviceView is one device's page and GET /api/web/v1/devices/{id}:
// its row as the devices page shows it, and its newest inventory.
type deviceView struct {
	AsOf        string    `json:"as_of"`
	LakeRelease string    `json:"lake_release,omitempty"`
	Device      deviceRow `json:"device"`
	// Inventory is null until the device's agent sends one.
	Inventory *inventoryView `json:"inventory"`
	// RefusedOnly lists the refused projects alone. The page takes it
	// as ?show=refused; the API always lists every project.
	RefusedOnly bool `json:"-"`
	// Actions offers the operator's forms, as on the devices page.
	Actions  bool     `json:"-"`
	CSRF     string   `json:"-"`
	Profiles []string `json:"-"`
	Problem  string   `json:"-"`
	// Notice says what a save that came back to this page saved.
	Notice string `json:"-"`
	// Hidden holds the keys, as ProjectKey.String writes them, of the
	// projects hidden from review.
	Hidden map[string]bool `json:"-"`
}

// inventoryView is a device's newest inventory. Allowed and Refused
// count the projects listed; a strict device lists no refused project,
// and RefusedSessions is all it says of them.
type inventoryView struct {
	Mode            string                      `json:"mode"`
	GeneratedAt     string                      `json:"generated_at"`
	ReceivedAt      string                      `json:"received_at"`
	Projects        []protocol.InventoryProject `json:"projects"`
	Allowed         int                         `json:"allowed"`
	Refused         int                         `json:"refused"`
	RefusedSessions int                         `json:"refused_sessions"`
	RefusedBytes    int64                       `json:"refused_bytes"`
	Truncated       bool                        `json:"truncated,omitempty"`
	// Shown is the projects the page lists: all of them, or the refused
	// ones alone.
	Shown []protocol.InventoryProject `json:"-"`
}

// Strict reports whether the device holds back its refused projects.
func (v *inventoryView) Strict() bool { return v.Mode == protocol.InventoryStrict }

func deviceURL(id string) string { return devicesPath + "/" + url.PathEscape(id) }

func (s *Server) device(w http.ResponseWriter, r *http.Request) {
	if len(r.URL.Query()) != 0 {
		fail(w, r, catalog.ErrPage)
		return
	}
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readDevice(ctx, r.PathValue("id"), s.now())
	if errors.Is(err, catalog.ErrNoDevice) {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		fail(w, r, err)
		return
	}
	writeJSON(w, v)
}

func (s *Server) devicePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	saved, present := savedQuery(q)
	if present {
		q.Del("saved")
		q.Del("revision")
	}
	refused := q.Get("show") == "refused"
	if len(q) > 1 || len(q) == 1 && !refused || len(q["show"]) > 1 {
		pageError(w, r, catalog.ErrPage)
		return
	}
	s.renderDeviceSaved(w, r, r.PathValue("id"), refused, "", saved, http.StatusOK)
}

// renderDevice shows device id with problem, after an operator's
// action on it was refused.
func (s *Server) renderDevice(w http.ResponseWriter, r *http.Request, id string, refused bool, problem string, status int) {
	s.renderDeviceSaved(w, r, id, refused, problem, nil, status)
}

// renderDeviceSaved is renderDevice after a save of the saved profile
// revisions came back to the page. The notice names only the profile
// the device uses.
func (s *Server) renderDeviceSaved(w http.ResponseWriter, r *http.Request, id string, refused bool, problem string, saved []savedRef, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	v, err := s.readDevice(ctx, id, s.now())
	if errors.Is(err, catalog.ErrNoDevice) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		pageError(w, r, err)
		return
	}
	// A strict device lists no refused project, so it has no refused
	// view: filtering would claim nothing is refused.
	if v.Inventory != nil && v.Inventory.Strict() {
		refused = false
	}
	v.RefusedOnly, v.Problem = refused, problem
	v.Notice = s.savedNotice(r, saved, func(name string) bool { return name == v.Device.Profile })
	if iv := v.Inventory; iv != nil {
		iv.Shown = iv.Projects
		if refused {
			iv.Shown = nil
			for _, p := range iv.Projects {
				if !p.Allowed {
					iv.Shown = append(iv.Shown, p)
				}
			}
		}
	}
	hides, err := s.catalog.HiddenProjects(ctx)
	if err != nil {
		pageError(w, r, err)
		return
	}
	v.Hidden = map[string]bool{}
	for _, h := range hides {
		v.Hidden[h.Key.String()] = true
	}
	if ident, csrf := webauth.Current(r); ident.Operator && s.reg != nil {
		v.Actions, v.CSRF = true, csrf
		if v.Profiles, err = s.reg.Lake().Catalog.ProfileNames(ctx); err != nil {
			s.logError(r, "listing profiles failed", err)
			v.Profiles = []string{config.DefaultProfile}
		}
	}
	var urgent []string
	if a := v.Device.Advisory; a != nil && a.Severity == advisory.Urgent && v.Device.State == "active" {
		urgent = []string{v.Device.Name}
	}
	renderStatus(w, r, pageData{Title: v.Device.Name, View: "device", AsOf: v.AsOf, Device: v, Urgent: urgent}, status)
}

// readDevice is device id's row, read as the devices page reads every
// row so the two cannot disagree, and its newest inventory.
func (s *Server) readDevice(ctx context.Context, id string, now time.Time) (deviceView, error) {
	all, err := s.readDevices(ctx, now)
	if err != nil {
		return deviceView{}, err
	}
	v := deviceView{AsOf: all.AsOf, LakeRelease: all.LakeRelease}
	found := false
	for _, row := range all.Devices {
		if row.ID == id {
			v.Device, found = row, true
			break
		}
	}
	if !found {
		return deviceView{}, catalog.ErrNoDevice
	}
	inv, ok, err := s.catalog.DeviceInventoryOf(ctx, id)
	if err != nil || !ok {
		return v, err
	}
	iv := &inventoryView{
		Mode:            inv.Inventory.Mode,
		GeneratedAt:     inv.Inventory.GeneratedAt.UTC().Format(time.RFC3339),
		ReceivedAt:      inv.Received.UTC().Format(time.RFC3339),
		Projects:        inv.Inventory.Projects,
		RefusedSessions: inv.Inventory.RefusedSessions,
		RefusedBytes:    inv.Inventory.RefusedBytes,
		Truncated:       inv.Inventory.Truncated,
	}
	if iv.Projects == nil {
		iv.Projects = []protocol.InventoryProject{}
	}
	for _, p := range iv.Projects {
		if p.Allowed {
			iv.Allowed++
		} else {
			iv.Refused++
		}
	}
	v.Inventory = iv
	return v, nil
}
