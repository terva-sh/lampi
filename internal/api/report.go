package api

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// maxReportBytes caps a heartbeat. A report is a few hundred bytes; the
// cap leaves room for a long error.
const maxReportBytes int64 = 64 << 10

// Field caps. The dashboard shows these strings, so a report cannot
// make the catalog hold more than a screen of any one of them.
const (
	maxReportField = 256
	maxReportError = 2 << 10
)

// agentReport stores the calling device's heartbeat as its newest
// report. A lake with no device tokens takes anyone's report, as it
// takes anyone's upload, and has no device to file it under, so it
// answers as if stored and keeps nothing.
func (s *Server) agentReport(w http.ResponseWriter, r *http.Request) {
	var rep protocol.AgentReport
	if !s.decodeJSON(w, r, &rep, maxReportBytes, "") {
		return
	}
	now := s.now().UTC()
	d, ok := r.Context().Value(deviceKey{}).(catalog.Device)
	if !ok {
		writeJSON(w, http.StatusOK, protocol.AgentReportResponse{ReceivedAt: now})
		return
	}
	clampReport(&rep)
	if err := s.Catalog.PutDeviceReport(r.Context(), d.ID, rep, now); err != nil {
		s.fail(w, r, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, protocol.AgentReportResponse{ReceivedAt: now})
}

// clampReport cuts each string in rep to its cap on a rune boundary.
func clampReport(rep *protocol.AgentReport) {
	for _, f := range []*string{&rep.AgentVersion, &rep.MachineID, &rep.Inventory, &rep.Profile, &rep.ProfileVersion, &rep.AllowSource, &rep.DenySource} {
		*f = clamp(*f, maxReportField)
	}
	rep.LastError = clamp(rep.LastError, maxReportError)
}

func clamp(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
