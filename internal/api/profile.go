package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

// SetProfiles replaces the profiles agents fetch. A request in flight
// keeps the set it read.
func (s *Server) SetProfiles(p config.Profiles) { s.profiles.Store(&p) }

// Profiles is the set agents fetch now.
func (s *Server) Profiles() config.Profiles {
	if p := s.profiles.Load(); p != nil {
		return *p
	}
	return config.Profiles{config.DefaultProfile: {}}
}

// agentConfig returns the calling device's profile, signed. A device
// with no profile gets the default. A lake with no device tokens has no
// device to ask about and serves the default too. A device whose
// profile has left the profiles file gets 404, and the agent keeps the
// copy it has.
func (s *Server) agentConfig(w http.ResponseWriter, r *http.Request) {
	if s.Identity == nil {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("this lake has no identity"))
		return
	}
	d, _ := r.Context().Value(deviceKey{}).(catalog.Device)
	name := d.Profile
	if name == "" {
		name = config.DefaultProfile
	}
	p, ok := s.Profiles()[name]
	if !ok {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("profile %s is not in the lake's profiles file", name))
		return
	}
	raw, err := json.Marshal(p)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	now := s.now()
	signed, err := s.Identity.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID:   s.Identity.LakeID,
		DeviceID: d.ID,
		Profile:  name,
		Version:  p.Version(),
		IssuedAt: now,
		Config:   raw,
	}, now)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, signed)
}
