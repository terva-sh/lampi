package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/registrar"
	"terva.sh/lampi/internal/webauth"
)

// Registrations lets operators mint, list and revoke registration codes
// from the dashboard. Without it those routes do not exist.
type Registrations struct {
	// Lake returns the lake as it is now, since SIGHUP can replace its
	// identity and profiles.
	Lake func() registrar.Lake
	// Release is the tag the lake was built from, such as v0.1.1, or ""
	// for a build from no tag. The install line pins install.sh to it.
	Release string
}

// DefaultCodeLifetime is the expiry the dashboard offers first. A code
// in a copied install line should not outlive the copy by much.
const DefaultCodeLifetime = time.Hour

// CSRFHeader carries the session's CSRF token on API writes.
const CSRFHeader = "X-Lampi-CSRF"

// codeView is a registration code as the dashboard shows it. It never
// holds the secret.
type codeView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	State      string `json:"state"`
	Profile    string `json:"profile"`
	Created    string `json:"created"`
	Expires    string `json:"expires"`
	CreatedBy  string `json:"created_by"`
	Revoked    string `json:"revoked,omitempty"`
	RevokedBy  string `json:"revoked_by,omitempty"`
	Used       string `json:"used,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
}

func viewCode(r catalog.Registration, devices map[string]string, now time.Time) codeView {
	v := codeView{ID: r.ID, Name: r.Name, State: r.State(now), Profile: r.Profile, Created: stampOf(r.Created), Expires: stampOf(r.Expires),
		CreatedBy: r.CreatedBy, Revoked: stampOf(r.Revoked), RevokedBy: r.RevokedBy, Used: stampOf(r.Used), DeviceID: r.DeviceID, DeviceName: devices[r.DeviceID]}
	if v.Profile == "" {
		v.Profile = config.DefaultProfile
	}
	return v
}

func stampOf(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// mintedView is the one response that carries a code.
type mintedView struct {
	Registration codeView `json:"registration"`
	Code         string   `json:"code"`
	LakeID       string   `json:"lake_id"`
	Fingerprint  string   `json:"fingerprint"`
	// Install installs terva-lampi and registers the machine with the
	// code. Pinned says install.sh and the binary are the lake's own
	// release; otherwise they are the latest release.
	Install string `json:"install_command"`
	Pinned  bool   `json:"install_pinned"`
}

var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// installCommand is the line an operator copies to a new machine. It
// begins with a space so shells that skip such lines keep it out of
// history. The code travels in TERVA_LAMPI_CODE, which install.sh hands
// to register on stdin or in a private file, never as an argument.
func installCommand(release, code, fingerprint string) (string, bool) {
	ref, version, pinned := "main", "", false
	if releaseTag.MatchString(release) {
		ref, version, pinned = release, " --version "+release, true
	}
	return " curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/" + ref + "/install.sh | TERVA_LAMPI_CODE='" + code + "' sh -s --" + version + " --register --fingerprint " + fingerprint, pinned
}

// actor names the signed-in operator in the catalog and the audit log.
func actor(id webauth.Identity) registrar.Actor {
	a := "web:" + id.Subject
	if id.Display != "" && id.Display != id.Subject {
		a += " (" + id.Display + ")"
	}
	a = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, a)
	if len(a) > 256 {
		a = strings.ToValidUTF8(a[:256], "")
	}
	return registrar.Actor{Catalog: a, Audit: a}
}

// mintLimit bounds how often the dashboard attempts a mint that passed
// its input checks. Each attempt fetches the key list through the
// public URL, and a mint appends to the audit log.
type mintLimit struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
}

const (
	mintBurst = 5
	mintEvery = 12 * time.Second
)

func (l *mintLimit) allow(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last.IsZero() {
		l.tokens = mintBurst
	} else if d := now.Sub(l.last); d > 0 {
		l.tokens = min(mintBurst, l.tokens+d.Seconds()/mintEvery.Seconds())
	}
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

func (s *Server) registrationRoutes(m *http.ServeMux) {
	if s.reg == nil {
		return
	}
	op := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.OperatorOnly(h)) }
	m.Handle("GET /api/web/v1/registrations", op(s.listCodes))
	m.Handle("POST /api/web/v1/registrations", op(s.mintCode))
	m.Handle("POST /api/web/v1/registrations/{id}/revoke", op(s.revokeCode))
	s.registrationPages(m)
}

func (s *Server) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func apiError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

// codes lists every code, newest first. It records the expiries since
// the last look, as serve register --list does.
func (s *Server) codes(r *http.Request, now time.Time) ([]codeView, error) {
	ctx, cancel := readContext(r)
	defer cancel()
	id, _ := webauth.Current(r)
	l := s.reg.Lake()
	regs, err := registrar.List(ctx, l, actor(id).Audit, now)
	if errors.Is(err, registrar.ErrAuditQueued) {
		s.logError(r, "registration expiries are queued for the audit log", err)
	} else if err != nil {
		return nil, err
	}
	devs, err := l.Catalog.Devices(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(devs))
	for _, d := range devs {
		names[d.ID] = d.Name
	}
	out := make([]codeView, 0, len(regs))
	for _, reg := range regs {
		out = append(out, viewCode(reg, names, now))
	}
	return out, nil
}

func (s *Server) listCodes(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		fail(w, catalog.ErrPage)
		return
	}
	now := s.now()
	items, err := s.codes(r, now)
	if err != nil {
		s.logError(r, "listing registration codes failed", err)
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"items": items, "as_of": stampOf(now)})
}

// mintRequest is the body of a mint. Expires is a Go duration such as
// 1h or 72h; empty is DefaultCodeLifetime.
type mintRequest struct {
	Name    string `json:"name"`
	Profile string `json:"profile"`
	Expires string `json:"expires"`
}

// errFresh is a mint by an operator whose sign-in is older than
// webauth.FreshWindow.
var errFresh = errors.New("sign in again to mint")

// mint checks and mints one code for the signed-in operator. Its errors
// are mapped by mintStatus.
func (s *Server) mint(r *http.Request, req mintRequest) (mintedView, error) {
	now := s.now()
	if !webauth.Fresh(r, now) {
		return mintedView{}, errFresh
	}
	lifetime := DefaultCodeLifetime
	if req.Expires != "" {
		d, err := time.ParseDuration(req.Expires)
		if err != nil {
			return mintedView{}, registrar.ErrLifetime
		}
		lifetime = d
	}
	if lifetime <= 0 || lifetime > registrar.MaxLifetime {
		return mintedView{}, registrar.ErrLifetime
	}
	if req.Name == "" || catalog.DeviceName(req.Name) != req.Name {
		return mintedView{}, errBadName
	}
	lake := s.reg.Lake()
	known, err := lake.Catalog.HasProfile(r.Context(), req.Profile)
	if err != nil {
		return mintedView{}, err
	}
	if !known {
		return mintedView{}, registrar.ErrNoProfile
	}
	// Input is checked before the limit, so a mistyped form does not use
	// it up. What the limit bounds is the key-list fetch and the synced
	// audit line an attempt past this point costs, whether or not it
	// ends in a code.
	if !s.mints.allow(now) {
		return mintedView{}, errMintRate
	}
	id, _ := webauth.Current(r)
	m, err := registrar.Mint(r.Context(), lake, req.Name, req.Profile, lifetime, actor(id), now)
	if err != nil {
		return mintedView{}, err
	}
	v := mintedView{Registration: viewCode(m.Registration, nil, now), Code: m.Code, LakeID: m.LakeID, Fingerprint: m.Fingerprint}
	v.Install, v.Pinned = installCommand(s.reg.Release, m.Code, m.Fingerprint)
	return v, nil
}

var (
	errBadName  = errors.New("a device name is lowercase letters, digits, '.', '-' and '_', at most 64")
	errMintRate = errors.New("too many codes minted just now; wait a minute")
)

// mintStatus maps a mint error to a status and an error code. Anything
// unnamed is the lake's own trouble and is logged, not shown.
func mintStatus(err error) (int, string) {
	var urlErr *registrar.URLError
	switch {
	case errors.Is(err, errFresh):
		return http.StatusForbidden, "fresh_login_required"
	case errors.Is(err, errBadName):
		return http.StatusBadRequest, "invalid_name"
	case errors.Is(err, registrar.ErrLifetime):
		return http.StatusBadRequest, "invalid_expiry"
	case errors.Is(err, registrar.ErrNoProfile):
		return http.StatusBadRequest, "unknown_profile"
	case errors.Is(err, catalog.ErrNameTaken):
		return http.StatusConflict, "name_taken"
	case errors.Is(err, errMintRate):
		return http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, registrar.ErrNoIdentity), errors.Is(err, registrar.ErrNoURL), errors.As(err, &urlErr):
		return http.StatusServiceUnavailable, "lake_not_ready"
	}
	return http.StatusInternalServerError, "mint_failed"
}

func (s *Server) mintCode(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	// A pointer, so a body of null is told apart from an empty object.
	var req *mintRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req == nil || !errors.Is(dec.Decode(new(json.RawMessage)), io.EOF) {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	v, err := s.mint(r, *req)
	if err != nil {
		status, code := mintStatus(err)
		if status >= 500 {
			s.logError(r, "minting a registration code failed", err)
		}
		if code == "fresh_login_required" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "login": webauth.FreshLoginURL(adminRegistrationsPath)})
			return
		}
		apiError(w, status, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(v)
}

// adminRegistrationsPath is the dashboard page for codes.
const adminRegistrationsPath = "/admin/registrations"

// revoke cancels a pending code by id for the signed-in operator.
func (s *Server) revoke(r *http.Request, ref string) (codeView, int, string) {
	id, _ := webauth.Current(r)
	now := s.now()
	reg, err := registrar.Revoke(r.Context(), s.reg.Lake(), ref, actor(id), now)
	switch {
	case errors.Is(err, catalog.ErrNoRegistration):
		return codeView{}, http.StatusNotFound, "not_found"
	case errors.Is(err, catalog.ErrRegistrationUsed):
		return codeView{}, http.StatusConflict, "already_used"
	case errors.Is(err, catalog.ErrRegistrationRevoked):
		return viewCode(reg, nil, now), http.StatusConflict, "already_revoked"
	case err != nil && reg.ID != "":
		// The revoke stands; the audit line did not land.
		s.logError(r, "revoked a registration code but the audit line failed", err)
		return viewCode(reg, nil, now), http.StatusInternalServerError, "audit_failed"
	case err != nil:
		s.logError(r, "revoking a registration code failed", err)
		return codeView{}, http.StatusInternalServerError, "revoke_failed"
	}
	return viewCode(reg, nil, now), http.StatusOK, ""
}

func (s *Server) revokeCode(w http.ResponseWriter, r *http.Request) {
	if !s.auth.CheckWrite(r, r.Header.Get(CSRFHeader)) {
		apiError(w, http.StatusForbidden, "csrf_failed")
		return
	}
	// Only ids here: a name could match a code the operator did not see.
	ref := r.PathValue("id")
	if !strings.HasPrefix(ref, "reg_") {
		apiError(w, http.StatusNotFound, "not_found")
		return
	}
	v, status, code := s.revoke(r, ref)
	if code != "" && v.ID == "" {
		apiError(w, status, code)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"registration": v}
	if code != "" {
		body["error"] = code
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) logError(r *http.Request, msg string, err error) {
	if s.log != nil {
		s.log.ErrorContext(r.Context(), msg, "err", err)
	}
}
