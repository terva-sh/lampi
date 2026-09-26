package api

import (
	"encoding/json"
	"errors"
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
	if s.Identity() == nil {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("this lake has no identity"))
		return
	}
	d, _ := r.Context().Value(deviceKey{}).(catalog.Device)
	signed, err := s.signedProfile(d)
	if errors.Is(err, errNoProfile) {
		s.fail(w, r, http.StatusNotFound, err)
		return
	}
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, signed)
}

var errNoProfile = errors.New("profile is not in the lake's profiles file")

// signedProfile is device d's profile signed for the agent.
func (s *Server) signedProfile(d catalog.Device) (*protocol.Signed, error) {
	name := d.Profile
	if name == "" {
		name = config.DefaultProfile
	}
	p, ok := s.Profiles()[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errNoProfile, name)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	now := s.now()
	id := s.Identity()
	if id == nil {
		return nil, errors.New("this lake has no identity")
	}
	return id.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID:   id.LakeID,
		DeviceID: d.ID,
		Profile:  name,
		Version:  p.Version(),
		IssuedAt: now,
		Config:   raw,
	}, now)
}
