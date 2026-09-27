package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/regcode"
)

// maxRegisterBytes bounds the register body: a secret, a hash, a
// machine id and a name.
const maxRegisterBytes = 4 << 10

var tokenHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// errRefused is the one answer to a code that is unknown, used, expired
// or revoked, so a caller cannot tell which codes exist. The audit log
// keeps the reason.
var errRefused = errors.New("registration refused: the code is not valid; ask the lake operator for a new one")

// register redeems a registration code without a token. It is
// rate-limited with the other open routes. The secret is never logged:
// the access log has no body, errors do not quote it, and the audit log
// names the registration by id.
//
// Every attempt the limiter lets through and the lake refuses gets a
// registration.refused line with the reason. One the limiter refuses does
// not: each audit line is a synced write, and the limiter is what bounds
// how many an open route can cause.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	if !s.openLimit().allow(s.now()) {
		w.Header().Set("Retry-After", "1")
		s.fail(w, r, http.StatusTooManyRequests, errors.New("too many requests; try again"))
		return
	}
	if s.Identity == nil {
		s.auditRefusal("", "", "lake has no identity")
		s.fail(w, r, http.StatusNotFound, errors.New("this lake has no identity"))
		return
	}
	// A lake that takes requests without a token has nothing for a new
	// token to open, and its first registered device would close it to
	// every client already using it.
	if s.Devices == nil || s.Devices.Empty() {
		s.auditRefusal("", "", "lake accepts requests without a token")
		s.fail(w, r, http.StatusConflict, errors.New("this lake accepts requests without a token; start serve with --token-file before registering devices"))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxRegisterBytes+1))
	if err != nil {
		s.auditRefusal("", "", "body could not be read")
		code, berr := bodyStatus(err)
		note(r, err)
		writeJSON(w, code, protocol.ErrorBody{Error: berr.Error()})
		return
	}
	if len(raw) > maxRegisterBytes {
		s.auditRefusal("", "", "body too large")
		s.fail(w, r, http.StatusRequestEntityTooLarge, fmt.Errorf("request body exceeds %d bytes", maxRegisterBytes))
		return
	}
	var req protocol.RegisterRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		s.auditRefusal("", "", "body is not a register request")
		s.fail(w, r, http.StatusBadRequest, errors.New("body is not a register request"))
		return
	}
	switch {
	case !regcode.ValidSecret(req.Secret):
		s.refuseRegistration(w, r, "", "malformed secret")
		return
	case !tokenHashPattern.MatchString(req.TokenSHA256):
		s.auditRefusal("", "", "malformed token_sha256")
		s.fail(w, r, http.StatusBadRequest, errors.New("token_sha256 is 64 lowercase hex characters"))
		return
	case req.MachineID == "" || len(req.MachineID) > 128:
		s.auditRefusal("", "", "malformed machine_id")
		s.fail(w, r, http.StatusBadRequest, errors.New("machine_id is required, at most 128 characters"))
		return
	}
	d, reg, err := s.Catalog.Redeem(r.Context(), regcode.HashSecret(req.Secret), req.TokenSHA256, req.MachineID, s.now())
	switch {
	case errors.Is(err, catalog.ErrRegistrationUnknown):
		s.refuseRegistration(w, r, "", "unknown")
		return
	case errors.Is(err, catalog.ErrRegistrationUsed):
		s.refuseRegistration(w, r, reg.ID, "used")
		return
	case errors.Is(err, catalog.ErrRegistrationExpired):
		s.auditExpiry(r.Context(), reg.ID)
		s.refuseRegistration(w, r, reg.ID, "expired")
		return
	case errors.Is(err, catalog.ErrRegistrationRevoked):
		s.refuseRegistration(w, r, reg.ID, "revoked")
		return
	case errors.Is(err, catalog.ErrTokenTaken):
		// The agent makes a fresh token; a clash is a replay or a bug.
		s.refuseRegistration(w, r, reg.ID, "token already belongs to a device")
		return
	case errors.Is(err, catalog.ErrMachineTaken):
		s.auditRefusal(reg.ID, req.MachineID, "machine_id bound to another device")
		s.fail(w, r, http.StatusConflict, fmt.Errorf("machine_id %s is bound to another device; run serve devices unbind on the lake, or register from a new machine id", req.MachineID))
		return
	case err != nil:
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	detail := "registration=" + reg.ID
	if req.Name != "" {
		detail += " suggested_name=" + catalog.DeviceName(req.Name)
	}
	s.audit(audit.Event{Kind: audit.RegistrationRedeemed, Device: d.Name, DeviceID: d.ID, MachineID: d.MachineID, Detail: detail})
	s.audit(audit.Event{Kind: audit.DeviceCreated, Device: d.Name, DeviceID: d.ID, Detail: "source=" + d.Source})
	s.audit(audit.Event{Kind: audit.DeviceBound, Device: d.Name, DeviceID: d.ID, MachineID: d.MachineID})
	if info := infoOf(r); info != nil {
		info.device, info.deviceID = d.Name, d.ID
	}
	resp := protocol.RegisterResponse{DeviceID: d.ID, Name: d.Name, LakeID: s.Identity.LakeID}
	// The device exists now. A profile that cannot be signed is logged,
	// and the agent fetches it later.
	if signed, err := s.signedProfile(d); err == nil {
		resp.Config = signed
	} else {
		s.logger().Warn("register: profile", "device", d.Name, "err", err)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, resp)
}

// auditExpiry writes the expiry of the code just presented, the first
// time the lake sees it expired. The catalog hands each code out once,
// so this route adds at most one line per code ever minted.
func (s *Server) auditExpiry(ctx context.Context, regID string) {
	regs, err := s.Catalog.RecordExpiries(ctx, s.now(), regID)
	if err != nil {
		s.logger().Error("register: record expiry", "registration", regID, "err", err)
		return
	}
	for _, reg := range regs {
		s.audit(audit.Event{Kind: audit.RegistrationExpired, Device: reg.Name,
			Detail: "registration=" + reg.ID + " expires=" + reg.Expires.Format(time.RFC3339)})
	}
}

func (s *Server) refuseRegistration(w http.ResponseWriter, r *http.Request, regID, reason string) {
	s.auditRefusal(regID, "", reason)
	s.fail(w, r, http.StatusForbidden, errRefused)
}

// auditRefusal records a refused attempt. reason is the lake's own
// wording, never text from the request, so the secret cannot reach it.
func (s *Server) auditRefusal(regID, machineID, reason string) {
	detail := "reason=" + reason
	if regID != "" {
		detail = "registration=" + regID + " " + detail
	}
	s.audit(audit.Event{Kind: audit.RegistrationRefused, MachineID: machineID, Detail: detail})
}
