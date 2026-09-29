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
// one: a device's page, with its refused filter if it had one.
func returnPath(raw string) string {
	if raw == "" || strings.ContainsAny(raw, "\\\r\n\t") || strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Fragment != "" || u.RawPath != "" {
		return ""
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
	id := strings.TrimPrefix(u.Path, devicesPath+"/")
	if d, err := deviceByID(r.Context(), s.catalog, id); err == nil {
		return d.Name
	}
	return "the device"
}

// savedURL is path with the profile revision a save made, for the page
// to say what was saved.
func savedURL(path string, p catalog.Profile) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return path + sep + "saved=" + url.QueryEscape(p.Name) + "&revision=" + strconv.FormatInt(p.Revision, 10)
}

// savedNotice reads ?saved=NAME&revision=N off a page a save came back
// to. It says what that revision saved, from the catalog rather than
// the query, so a link cannot make the page claim a save that did not
// happen. ok is false when the query does not name a revision.
func (s *Server) savedNotice(r *http.Request, q url.Values) (notice string, ok bool) {
	name, rev := q.Get("saved"), q.Get("revision")
	if len(q["saved"]) != 1 || len(q["revision"]) != 1 || !config.ValidProfileName(name) {
		return "", false
	}
	id, err := strconv.ParseInt(rev, 10, 64)
	if err != nil || id < 1 {
		return "", false
	}
	pr, err := s.catalog.ProfileRevisionByID(r.Context(), name, id)
	if err != nil || pr.Deleted {
		return "", true
	}
	notice = "Saved profile " + name + " as revision " + rev
	if pr.Note != "" {
		notice += ": " + pr.Note
	}
	return notice + ". Devices on it fetch it within seconds; this page shows the change once the device sends a new inventory.", true
}
