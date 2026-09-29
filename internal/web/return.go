package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

// A page that sends the operator into the profile editor names where
// to come back to, as the form field "return". The editor carries it
// through preview and save, offers Back to it, and a save redirects to
// it with the revision it made. Only pages the dashboard serves are
// accepted, so the field cannot send a browser anywhere else.

// returnPath is raw as a page to come back to, or "" when it is not
// one: a device's page, with its refused filter if it had one, or the
// review queue with its filter.
func returnPath(raw string) string {
	if raw == "" || strings.ContainsAny(raw, "\\\r\n\t#") || strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return ""
	}
	if u.Path == reviewPath {
		// Query would drop a malformed pair and accept the rest.
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return ""
		}
		f, ok := parseReviewFilter(q)
		if !ok {
			return ""
		}
		return f.URL()
	}
	id, ok := strings.CutPrefix(u.Path, devicesPath+"/")
	if !ok || !validDeviceID(id) {
		return ""
	}
	switch u.RawQuery {
	case "":
		return deviceURL(id)
	case "show=refused":
		return deviceURL(id) + "?show=refused"
	}
	return ""
}

// validDeviceID reports whether id has the form a device id has.
func validDeviceID(id string) bool {
	rest, ok := strings.CutPrefix(id, "dev_")
	if !ok || rest == "" || len(rest) > 64 {
		return false
	}
	for _, c := range rest {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// returnLabel names the page at path, a returnPath, for a Back link.
func (s *Server) returnLabel(r *http.Request, path string) string {
	u, _ := url.Parse(path)
	if u.Path == reviewPath {
		return "review"
	}
	id := strings.TrimPrefix(u.Path, devicesPath+"/")
	if d, err := deviceByID(r.Context(), s.catalog, id); err == nil {
		return d.Name
	}
	return "the device"
}

// savedRef names a profile revision a save made.
type savedRef struct {
	Name string
	Rev  int64
}

// maxSavedRefs bounds the revisions one page names; a batch Allow saves
// at most one per profile.
const maxSavedRefs = 32

// savedURL is path with the profile revisions a save made, for the page
// to say what was saved: the first maxSavedRefs of them, which is as
// many as savedQuery reads.
func savedURL(path string, ps ...catalog.Profile) string {
	ps = ps[:min(len(ps), maxSavedRefs)]
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for _, p := range ps {
		path += sep + "saved=" + url.QueryEscape(p.Name) + "&revision=" + strconv.FormatInt(p.Revision, 10)
		sep = "&"
	}
	return path
}

// savedQuery reads the saved=NAME&revision=N pairs off a page a save
// came back to. present is true when the query carries either key, well
// formed or not, so the page can take them off before reading the rest;
// a malformed set names nothing.
func savedQuery(q url.Values) (refs []savedRef, present bool) {
	if !q.Has("saved") && !q.Has("revision") {
		return nil, false
	}
	names, revs := q["saved"], q["revision"]
	if len(names) != len(revs) || len(names) > maxSavedRefs {
		return nil, true
	}
	for i, name := range names {
		rev, err := strconv.ParseInt(revs[i], 10, 64)
		if !config.ValidProfileName(name) || err != nil || rev < 1 {
			return nil, true
		}
		refs = append(refs, savedRef{name, rev})
	}
	return refs, true
}

// savedNotice says, for each ref whose profile keep accepts, who saved
// that revision, when, and with what note. It is read from the catalog
// and states only what the catalog records, so a link that names a
// revision cannot make the page claim anything that did not happen.
// "" when no ref names a saved revision.
func (s *Server) savedNotice(r *http.Request, refs []savedRef, keep func(string) bool) string {
	var lines []string
	for _, ref := range refs {
		if !keep(ref.Name) {
			continue
		}
		pr, err := s.catalog.ProfileRevisionByID(r.Context(), ref.Name, ref.Rev)
		if err != nil || pr.Deleted {
			continue
		}
		line := "Profile " + ref.Name + " revision " + strconv.FormatInt(ref.Rev, 10) + " was saved by " + pr.CreatedBy + " at " + pr.Created.UTC().Format("2006-01-02 15:04 UTC")
		if pr.Note != "" {
			line += ": " + pr.Note
		}
		lines = append(lines, line+".")
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, " ") + " Devices on it fetch it within seconds; a project shows the change once its device sends a new inventory."
}
