package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/catalog"
)

type deviceKey struct{}

// deviceOf is the device that authenticated r, if any. A lake with no
// device tokens authenticates nobody.
func deviceOf(r *http.Request) (catalog.Device, bool) {
	d, ok := r.Context().Value(deviceKey{}).(catalog.Device)
	return d, ok
}

// SyncDevices records the tokens in s.Devices as devices in the catalog.
func (s *Server) SyncDevices(ctx context.Context) error {
	return s.RecordDevices(ctx, s.Devices)
}

// RecordDevices records the tokens in set as devices in the catalog: new
// tokens become named devices, and tokens that left the file are marked
// detached. serve calls it at start, and on a reload before it publishes
// the new set, so no token is accepted before its device exists.
func (s *Server) RecordDevices(ctx context.Context, set *auth.Devices) error {
	var entries []catalog.TokenEntry
	for _, e := range set.Entries() {
		entries = append(entries, catalog.TokenEntry{Hash: e.Hash, Name: e.Name})
	}
	before, err := s.Catalog.Devices(ctx)
	if err != nil {
		return err
	}
	created, err := s.Catalog.SyncTokenFile(ctx, entries, s.now())
	if err != nil {
		return err
	}
	for _, d := range created {
		s.audit(audit.Event{Kind: audit.DeviceCreated, Device: d.Name, DeviceID: d.ID, Detail: "from the token file"})
	}
	after, err := s.Catalog.Devices(ctx)
	if err != nil {
		return err
	}
	was := map[string]bool{}
	for _, d := range before {
		was[d.ID] = !d.Detached.IsZero()
	}
	for _, d := range after {
		if !d.Detached.IsZero() && !was[d.ID] {
			s.audit(audit.Event{Kind: audit.DeviceDetached, Device: d.Name, DeviceID: d.ID, Detail: "token left the token file"})
		}
	}
	return nil
}

// audit writes e, an event that records no catalog change, to the
// lake's audit log. It goes through the catalog's outbox behind the
// events queued before it, so the log keeps the order they happened in
// and a line that cannot be written now is written later. When the
// catalog cannot queue it, it is appended directly. A lake opened
// without a data directory, as some tests do, keeps none. A failure is
// logged and does not fail the request.
func (s *Server) audit(e audit.Event) {
	if s.dataDir == "" {
		return
	}
	if e.Actor == "" {
		e.Actor = "serve"
	}
	if e.Time.IsZero() {
		e.Time = s.now()
	}
	if err := s.Catalog.QueueAudit(context.Background(), e.Time, e); err != nil {
		// A catalog that takes no write must not cost the line: append
		// it directly, after what is queued, which may write it out of
		// order but does not lose it.
		s.logger().Error("audit: queueing failed; appending directly", "kind", e.Kind, "err", err)
		s.flushAudit()
		if err := audit.Append(s.dataDir, e); err != nil {
			s.logger().Error("audit", "kind", e.Kind, "err", err)
		}
		return
	}
	s.flushAudit()
}

// flushAudit appends the queued audit events. One that cannot be
// written stays queued, is logged, and is tried again on the next
// flush: after the next change or event, and when serve starts.
func (s *Server) flushAudit() {
	if s.dataDir == "" {
		return
	}
	if err := s.Catalog.FlushAudit(context.Background(), s.dataDir); err != nil {
		s.logger().Error("audit: queued events stay queued", "err", err)
	}
}

// bindDevice binds the authenticating device to the manifest's machine
// the first time, and refuses a manifest from another machine. It
// reports false after writing the refusal.
func (s *Server) bindDevice(w http.ResponseWriter, r *http.Request, machineID string) bool {
	d, ok := deviceOf(r)
	if !ok {
		return true
	}
	// The row read at authentication may be stale: an operator can
	// unbind the device and another request bind it again in between.
	// Read the binding now.
	cur, found, err := s.Catalog.DeviceByHash(r.Context(), d.TokenSHA256)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return false
	}
	if !found || !cur.Revoked.IsZero() {
		s.fail(w, r, http.StatusUnauthorized, errors.New("unauthorized: this device is revoked"))
		return false
	}
	d = cur
	if d.MachineID == machineID || d.Source == catalog.DeviceFromAllow {
		return true
	}
	if d.MachineID != "" {
		return s.refuseDevice(w, r, d, machineID, fmt.Errorf("device %s is bound to another machine_id; an operator can run serve devices unbind %s", d.Name, d.Name))
	}
	bound, err := s.Catalog.BindMachine(r.Context(), d.ID, machineID)
	switch {
	case errors.Is(err, catalog.ErrMachineTaken):
		return s.refuseDevice(w, r, d, machineID, fmt.Errorf("machine_id %s belongs to another device", machineID))
	case errors.Is(err, catalog.ErrDeviceBound):
		return s.refuseDevice(w, r, d, machineID, fmt.Errorf("device %s is bound to another machine_id; an operator can run serve devices unbind %s", d.Name, d.Name))
	case err != nil:
		s.fail(w, r, http.StatusInternalServerError, err)
		return false
	}
	if bound {
		s.audit(audit.Event{Kind: audit.DeviceBound, Device: d.Name, DeviceID: d.ID, MachineID: machineID})
	}
	return true
}

func (s *Server) refuseDevice(w http.ResponseWriter, r *http.Request, d catalog.Device, machineID string, err error) bool {
	s.audit(audit.Event{Kind: audit.DeviceRefused, Device: d.Name, DeviceID: d.ID, MachineID: machineID, Detail: err.Error()})
	s.fail(w, r, http.StatusForbidden, err)
	return false
}
