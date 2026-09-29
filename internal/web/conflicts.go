package web

import (
	"context"
	"net/url"

	"terva.sh/lampi/internal/catalog"
)

// conflictsView is the Conflicts page and a session's Conflicts tab:
// the rows, what each machine is called, and the links that switch
// between open and every conflict (TKT-01M3PTMWF6).
type conflictsView struct {
	Records catalog.Page[catalog.Record]
	// Names maps a machine id to the device bound to it.
	Names    map[string]string
	Resolved bool
	// OpenURL and AllURL list the open conflicts and every conflict.
	OpenURL, AllURL string
	// Session is set on a session's tab, where the page names no
	// session per row.
	Session string
}

// resolutionLabels are how the page names each resolution.
var resolutionLabels = map[string]string{
	catalog.ResolutionKeptHead:     "Head kept",
	catalog.ResolutionMadeHead:     "Made the head",
	catalog.ResolutionSuperseded:   "Superseded",
	catalog.ResolutionNotAConflict: "Not a conflict",
}

func resolutionLabel(r string) string {
	if l, ok := resolutionLabels[r]; ok {
		return l
	}
	return r
}

// newConflictsView builds the view for records, listed under base. A
// device list that cannot be read leaves the machines unnamed rather
// than failing the page.
func (s *Server) newConflictsView(ctx context.Context, records catalog.Page[catalog.Record], p catalog.PageRequest, base url.URL, session string) conflictsView {
	v := conflictsView{Records: records, Resolved: p.Resolved, Session: session, Names: map[string]string{}}
	// Switching lists starts again from the first page.
	q := base.Query()
	q.Del("resolved")
	q.Del("cursor")
	// Only the path and query: a link never names another origin.
	v.OpenURL = (&url.URL{Path: base.Path, RawQuery: q.Encode()}).String()
	q.Set("resolved", "true")
	v.AllURL = (&url.URL{Path: base.Path, RawQuery: q.Encode()}).String()
	devices, err := s.catalog.Devices(ctx)
	if err != nil {
		return v
	}
	// A machine a revoked device was bound to keeps that name unless an
	// active device is bound to it now.
	for _, d := range devices {
		if _, named := v.Names[d.MachineID]; d.MachineID != "" && (!named || d.Revoked.IsZero()) {
			v.Names[d.MachineID] = d.Name
		}
	}
	return v
}
