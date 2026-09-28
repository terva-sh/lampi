package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

// The operator's changes to a device, as serve devices makes them:
// revoke, unbind, and profile, which sets the profile its agent
// fetches. Each is CSRF-checked, recorded with the signed-in operator
// as its actor, and written to the audit log before it answers.

const devicesPath = "/devices"

func (s *Server) deviceRoutes(m *http.ServeMux) {
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("POST /api/web/v1/devices/{id}/{action}", op(s.deviceActionAPI))
	m.Handle("POST "+devicesPath+"/{id}/{action}", op(s.deviceActionPage))
}

// deviceChange is what the API answers with after an action.
type deviceChange struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Profile   string `json:"profile"`
	MachineID string `json:"machine_id,omitempty"`
}

func viewChange(d catalog.Device) deviceChange {
	p := d.Profile
	if p == "" {
		p = config.DefaultProfile
	}
	return deviceChange{ID: d.ID, Name: d.Name, State: d.State(), Profile: p, MachineID: d.MachineID}
}

// deviceByID finds a device by its id. Only ids reach here: a name
// could match a device the operator did not see.
func deviceByID(ctx context.Context, cat *catalog.Catalog, id string) (catalog.Device, error) {
	if strings.HasPrefix(id, "dev_") {
		devices, err := cat.Devices(ctx)
		if err != nil {
			return catalog.Device{}, err
		}
		for _, d := range devices {
			if d.ID == id {
				return d, nil
			}
		}
	}
	return catalog.Device{}, catalog.ErrNoDevice
}

// changeDevice applies action to the device id for the signed-in
// operator. It returns the device as it now is, an HTTP status, and an
// error code, which is empty on success. A change that stands but whose
// audit line did not land returns the device with audit_failed.
func (s *Server) changeDevice(r *http.Request, id, action, profile string) (catalog.Device, int, string) {
	lake := s.reg.Lake()
	ctx := r.Context()
	d, err := deviceByID(ctx, lake.Catalog, id)
	switch {
	case errors.Is(err, catalog.ErrNoDevice):
		return catalog.Device{}, http.StatusNotFound, "not_found"
	case err != nil:
		s.logError(r, "reading devices failed", err)
		return catalog.Device{}, http.StatusInternalServerError, "action_failed"
	}
	if action != "revoke" && action != "unbind" && action != "profile" {
		return catalog.Device{}, http.StatusNotFound, "not_found"
	}
	if !d.Revoked.IsZero() {
		return d, http.StatusConflict, "revoked"
	}
	ident, _ := webauth.Current(r)
	who, now := actor(ident).Audit, s.now()
	switch action {
	case "revoke":
		d, err = lake.Catalog.RevokeDevice(ctx, d.Name, who, now)
	case "unbind":
		if d.MachineID == "" {
			return d, http.StatusConflict, "not_bound"
		}
		d, err = lake.Catalog.UnbindDevice(ctx, d.Name, who, now)
		d.MachineID = ""
	case "profile":
		if profile == "" {
			return d, http.StatusBadRequest, "invalid_request"
		}
		known, kerr := lake.Catalog.HasProfile(ctx, profile)
		if kerr != nil {
			s.logError(r, "reading profiles failed", kerr)
			return d, http.StatusInternalServerError, "action_failed"
		}
		if !known {
			return d, http.StatusBadRequest, "unknown_profile"
		}
		stored := profile
		if stored == config.DefaultProfile {
			stored = ""
		}
		d, err = lake.Catalog.SetDeviceProfile(ctx, d.Name, stored, profile, who, now)
	}
	if errors.Is(err, catalog.ErrNoDevice) {
		// Renamed or gone between the lookup and the change.
		return catalog.Device{}, http.StatusNotFound, "not_found"
	}
	if err != nil {
		s.logError(r, "changing a device failed", err)
		return catalog.Device{}, http.StatusInternalServerError, "action_failed"
	}
	// The change and its event committed together. A line that cannot
	// be written now stays queued for the next flush.
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		s.logError(r, "changed a device but the audit line failed", err)
		return d, http.StatusInternalServerError, "audit_failed"
	}
	return d, http.StatusOK, ""
}

func (s *Server) deviceActionAPI(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	// profile takes {"profile": NAME}; revoke and unbind take no body.
	var req struct {
		Profile string `json:"profile"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if r.PathValue("action") != "profile" && req.Profile != "" {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	d, status, code := s.changeDevice(r, r.PathValue("id"), r.PathValue("action"), req.Profile)
	if d.ID == "" {
		apiError(w, status, code)
		return
	}
	body := map[string]any{"device": viewChange(d)}
	if code != "" {
		body["error"] = code
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// deviceProblems says what to do about each refusal the page can meet.
var deviceProblems = map[string]string{
	"not_found":       "There is no such device.",
	"revoked":         "That device is revoked. Revoking is final, so nothing else can change it.",
	"not_bound":       "That device is not bound to a machine, so there is nothing to unbind.",
	"invalid_request": "Choose a profile.",
	"unknown_profile": "That profile is not in the lake any more. Choose another.",
	"audit_failed":    "The change stands, but writing it to the audit log failed. The line stays queued. Operator logs hold the details.",
	"action_failed":   "The change failed. Operator logs hold the details.",
}

func (s *Server) deviceActionPage(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r) {
		return
	}
	d, status, code := s.changeDevice(r, r.PathValue("id"), r.PathValue("action"), r.PostForm.Get("profile"))
	if code == "" {
		http.Redirect(w, r, devicesPath+"#"+d.ID, http.StatusSeeOther)
		return
	}
	s.renderDevices(w, r, deviceProblems[code], status)
}
