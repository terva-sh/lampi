package api

import (
	"errors"
	"net/http"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// maxInventoryBytes caps an inventory: MaxInventoryProjects rows with
// long paths fit.
const maxInventoryBytes int64 = 2 << 20

// maxInventoryPath caps a cwd or remote; the dashboard shows them.
const maxInventoryPath = 1 << 10

// agentInventory stores the calling device's inventory as its newest.
// A lake with no device tokens has no device to file it under, and
// answers as if stored.
func (s *Server) agentInventory(w http.ResponseWriter, r *http.Request) {
	var inv protocol.AgentInventory
	if !s.decodeJSON(w, r, &inv, maxInventoryBytes, "") {
		return
	}
	if inv.Mode != protocol.InventorySociable && inv.Mode != protocol.InventoryStrict {
		s.fail(w, r, http.StatusBadRequest, errors.New("inventory: mode must be sociable or strict"))
		return
	}
	now := s.now().UTC()
	d, ok := r.Context().Value(deviceKey{}).(catalog.Device)
	if !ok {
		writeJSON(w, http.StatusOK, protocol.AgentInventoryResponse{ReceivedAt: now})
		return
	}
	clampInventory(&inv)
	if err := s.Catalog.PutDeviceInventory(r.Context(), d.ID, inv, now); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.AgentInventoryResponse{ReceivedAt: now})
}

// clampInventory keeps what the lake stores within its caps, and holds
// a strict agent to its mode: a refused row it should not have sent is
// dropped, not kept.
func clampInventory(inv *protocol.AgentInventory) {
	out := inv.Projects[:0]
	for _, p := range inv.Projects {
		if inv.Mode == protocol.InventoryStrict && !p.Allowed {
			continue
		}
		if len(out) == protocol.MaxInventoryProjects {
			inv.Truncated = true
			break
		}
		p.GitRemote = clamp(p.GitRemote, maxInventoryPath)
		p.CWD = clamp(p.CWD, maxInventoryPath)
		p.CWDHash = clamp(p.CWDHash, maxReportField)
		p.Reason = clamp(p.Reason, maxReportField)
		if len(p.Harnesses) > 16 {
			p.Harnesses = p.Harnesses[:16]
		}
		for i := range p.Harnesses {
			p.Harnesses[i] = clamp(p.Harnesses[i], maxReportField)
		}
		out = append(out, p)
	}
	inv.Projects = out
	if inv.Projects == nil {
		inv.Projects = []protocol.InventoryProject{}
	}
}
