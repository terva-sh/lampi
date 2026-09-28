package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

// agentConfig returns the calling device's profile, signed. A device
// with no profile gets the default. A lake with no device tokens has no
// device to ask about and serves the default too. A device whose
// profile is not in the catalog gets 404, and the agent keeps the copy
// it has.
func (s *Server) agentConfig(w http.ResponseWriter, r *http.Request) {
	if s.Identity() == nil {
		s.fail(w, r, http.StatusNotFound, fmt.Errorf("this lake has no identity"))
		return
	}
	d, _ := r.Context().Value(deviceKey{}).(catalog.Device)
	signed, err := s.signedProfile(r.Context(), d)
	if errors.Is(err, catalog.ErrNoProfile) {
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

// signedProfile is device d's profile, resolved from the catalog and
// signed for the agent.
func (s *Server) signedProfile(ctx context.Context, d catalog.Device) (*protocol.Signed, error) {
	p, err := s.Catalog.ResolveProfile(ctx, d)
	if err != nil {
		return nil, err
	}
	return s.signProfile(s.Identity(), d, p)
}

// signProfile signs p for device d under the identity id, for a caller
// that must sign with the same identity it checked a key against.
func (s *Server) signProfile(id *identity.Identity, d catalog.Device, p catalog.EffectiveProfile) (*protocol.Signed, error) {
	raw, err := json.Marshal(p.Config)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if id == nil {
		return nil, errors.New("this lake has no identity")
	}
	return id.Sign(identity.ContextAgentConfig, protocol.AgentConfigPayload{
		LakeID:   id.LakeID,
		DeviceID: d.ID,
		Profile:  p.Name,
		Version:  p.Version,
		IssuedAt: now,
		Config:   raw,
		Layers:   p.Layers,
	}, now)
}
