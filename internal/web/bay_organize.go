package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/webauth"
)

const organizePath = "/admin/bays/organize"
const maxOrganizeForm = 25 << 20 // one URL-encoded 8 MiB artifact plus selection

var errPerSessionCohort = errors.New("session has no retained per-session migration edit")

type organizeForm struct {
	PerSession   *catalog.BayMigrationRequest
	DoctorPolicy string
	Filter       catalog.DiscoveryRequest
	Bays         string
	Focus        string
	Selected     string
	Exclude      string
	Mode         string
	Add          []string
	Remove       []string
	Proposed     string
	Reason       string
	Cursor       string
	History      []string
	RuleBay      string
	RuleAction   string
	RuleProposed bool
}

type organizeRow struct {
	catalog.SessionMetadata
	Selected bool
	Reasons  []string
}

type organizeView struct {
	Form              organizeForm
	Rows              []organizeRow
	Bays              []catalog.BayReference
	UnavailableAdd    []string
	UnavailableRemove []string
	Selected          []string
	Next              string
	Previous          bool
	Problem           string
	Done              string
	Fresh             bool
	FreshURL          string
	Plan              *catalog.BayMigrationPlan
	PlanJSON          string
	Receipt           *catalog.BayMigrationReceipt
	Verification      *catalog.MigrationVerification
	Rule              *catalog.BayRulePreview
	RuleCommand       string
	Doctor            *bayDoctorView
	ReleaseResults    []string
}

func (s *Server) organizationRoutes(m *http.ServeMux) {
	admin := func(h http.HandlerFunc) http.Handler { return s.auth.Guard(webauth.AdminOnly(h)) }
	m.Handle("GET "+organizePath, admin(s.organizePage))
	m.Handle("POST "+organizePath, admin(s.organizePost))
	m.Handle("GET "+organizePath+"/receipts/{id}", admin(s.organizeReceipt))
}

func initialOrganizeForm() organizeForm {
	return organizeForm{Bays: "default", Mode: "add", Remove: []string{catalog.DefaultBayID}, RuleAction: catalog.RuleAdd}
}

func (s *Server) organizePage(w http.ResponseWriter, r *http.Request) {
	f := initialOrganizeForm()
	q := r.URL.Query()
	for key, values := range q {
		if len(values) != 1 {
			fail(w, r, catalog.ErrPage)
			return
		}
		switch key {
		case "uid", "project", "cwd_prefix", "cwd_hash", "harness", "bays", "updated_since", "updated_until":
		default:
			fail(w, r, catalog.ErrPage)
			return
		}
	}
	f.Filter = catalog.DiscoveryRequest{Project: q.Get("project"), CWDPrefix: q.Get("cwd_prefix"), CWDHash: q.Get("cwd_hash"), Harness: q.Get("harness"), UpdatedSince: q.Get("updated_since"), UpdatedUntil: q.Get("updated_until")}
	if q.Has("bays") {
		f.Bays = q.Get("bays")
	}
	if uid := q.Get("uid"); uid != "" {
		f.Focus, f.Selected, f.Bays = uid, uid, ""
	}
	s.renderOrganize(w, r, organizeView{Form: f}, http.StatusOK)
}

func splitSessionText(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' ' })
}
func uniqueIDs(ids []string) []string {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return slices.Compact(ids)
}

func parseOrganizeForm(v url.Values) (organizeForm, error) {
	f := organizeForm{DoctorPolicy: v.Get("doctor_policy"), Filter: catalog.DiscoveryRequest{Harness: v.Get("harness"), Project: v.Get("project"), Unlinked: v.Get("unlinked") == "true", CWDPrefix: v.Get("cwd_prefix"), CWDHash: v.Get("cwd_hash"), Machine: v.Get("machine"), UpdatedSince: v.Get("updated_since"), UpdatedUntil: v.Get("updated_until")}, Bays: v.Get("bays"), Focus: v.Get("focus"), Selected: v.Get("selected"), Exclude: v.Get("exclude"), Mode: v.Get("mode"), Add: v["add"], Remove: v["remove"], Proposed: v.Get("proposed"), Reason: v.Get("reason"), Cursor: v.Get("cursor"), History: strings.Split(v.Get("history"), "\n"), RuleBay: v.Get("rule_bay"), RuleAction: v.Get("rule_action"), RuleProposed: v.Get("rule_proposed") == "true"}
	if raw := v.Get("per_session_request"); raw != "" {
		if len(raw) > 8<<20 {
			return f, catalog.ErrMigrationSize
		}
		var req catalog.BayMigrationRequest
		d := json.NewDecoder(strings.NewReader(raw))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF || len(req.Edits) == 0 || len(req.UIDs)+len(req.Add)+len(req.Remove) != 0 {
			return f, catalog.ErrMigrationInput
		}
		if len(req.Edits) > catalog.MigrationMaxSessions {
			return f, catalog.ErrSelectionLimit
		}
		f.PerSession = &req
		if f.Mode != "per-session" {
			return f, catalog.ErrMigrationInput
		}
	}
	if v.Get("history") == "" {
		f.History = nil
	}
	selected := splitSessionText(f.Selected)
	// The previous page is represented explicitly, so unticking its rows
	// removes them without losing choices from other pages.
	if f.Selected == v.Get("selection_base") {
		for _, uid := range v["page_uid"] {
			selected = slices.DeleteFunc(selected, func(s string) bool { return s == uid })
		}
		selected = append(selected, v["selected_page"]...)
	}
	selected = uniqueIDs(selected)
	f.Selected = strings.Join(selected, "\n")
	if len(selected) > catalog.MigrationMaxSessions || len(splitSessionText(f.Exclude)) > catalog.MigrationMaxSessions || len(f.History) > 1000 {
		return f, catalog.ErrSelectionLimit
	}
	for _, uid := range selected {
		if uid == "" || len(uid) > 128 {
			return f, catalog.ErrSelection
		}
	}
	if f.Mode == "" {
		f.Mode = "add"
	}
	if f.Mode != "add" && f.Mode != "move" && (f.Mode != "per-session" || f.PerSession == nil) {
		return f, catalog.ErrMigrationInput
	}
	return f, nil
}

func migrationRequestFromForm(f organizeForm) (catalog.BayMigrationRequest, error) {
	if f.PerSession == nil {
		req := catalog.BayMigrationRequest{UIDs: splitSessionText(f.Selected), Exclude: splitSessionText(f.Exclude), Add: slices.Clone(f.Add), Proposed: splitSessionText(f.Proposed), Reason: f.Reason}
		req.Add = append(req.Add, req.Proposed...)
		if f.Mode == "move" {
			if len(f.Remove) == 0 {
				return req, catalog.ErrMigrationInput
			}
			req.Remove = f.Remove
		}
		return req, nil
	}
	if len(f.Add)+len(f.Remove) != 0 || f.Proposed != "" || len(f.PerSession.Edits) > catalog.MigrationMaxSessions {
		return catalog.BayMigrationRequest{}, catalog.ErrMigrationInput
	}
	byUID := map[string]catalog.MigrationEdit{}
	for _, edit := range f.PerSession.Edits {
		if _, duplicate := byUID[edit.UID]; duplicate {
			return catalog.BayMigrationRequest{}, catalog.ErrMigrationInput
		}
		byUID[edit.UID] = edit
	}
	req := *f.PerSession
	req.Edits, req.Proposed = nil, nil
	req.Exclude, req.Reason = splitSessionText(f.Exclude), f.Reason
	for _, uid := range uniqueIDs(splitSessionText(f.Selected)) {
		edit, ok := byUID[uid]
		if !ok {
			return req, errPerSessionCohort
		}
		req.Edits = append(req.Edits, edit)
		if !slices.Contains(req.Exclude, uid) {
			for _, name := range f.PerSession.Proposed {
				if slices.Contains(edit.Add, name) {
					req.Proposed = append(req.Proposed, name)
				}
			}
		}
	}
	req.Proposed = uniqueIDs(req.Proposed)
	if len(req.Edits) == 0 {
		return req, catalog.ErrSelection
	}
	return req, nil
}

func (s *Server) organizationScope(ctx context.Context, f organizeForm) (catalog.Scope, error) {
	scope := catalog.AllBays()
	if f.PerSession != nil {
		ids := []string{}
		for _, edit := range f.PerSession.Edits {
			ids = append(ids, edit.UID)
		}
		scope = scope.OnlySessions(ids)
	}
	if f.Bays != "" {
		var err error
		scope, err = s.catalog.SelectReadBays(ctx, scope, f.Bays)
		if err != nil {
			return scope, err
		}
	}
	if f.Focus != "" {
		scope = scope.OnlySessions([]string{f.Focus})
	}
	return scope, nil
}

func (s *Server) renderOrganize(w http.ResponseWriter, r *http.Request, v organizeView, status int) {
	ctx, cancel := readContext(r)
	defer cancel()
	v.Selected = uniqueIDs(splitSessionText(v.Form.Selected))
	v.Fresh, v.FreshURL = webauth.Fresh(r, s.now()), webauth.FreshLoginURL(organizePath)
	bays, err := s.catalog.Bays(ctx)
	if err != nil {
		fail(w, r, err)
		return
	}
	for _, b := range bays {
		v.Bays = append(v.Bays, catalog.BayReference{ID: b.ID, Name: b.Name, Default: b.Default})
	}
	unavailable := func(refs []string) []string {
		var missing []string
		for _, ref := range refs {
			if !slices.ContainsFunc(v.Bays, func(b catalog.BayReference) bool { return b.ID == ref }) {
				missing = append(missing, ref)
			}
		}
		return missing
	}
	v.UnavailableAdd, v.UnavailableRemove = unavailable(v.Form.Add), unavailable(v.Form.Remove)
	scope, err := s.organizationScope(ctx, v.Form)
	if err == nil {
		req := v.Form.Filter
		req.Limit, req.Cursor = 50, v.Form.Cursor
		var page catalog.Page[catalog.SessionMetadata]
		page, err = s.catalog.DiscoverSessions(ctx, scope, req)
		if err == nil {
			v.Next, v.Previous = page.NextCursor, len(v.Form.History) > 0
			// Reuse the established inbox explanations for held/refused rows.
			entries, inboxErr := s.catalog.Inbox(ctx)
			if inboxErr != nil {
				err = inboxErr
			} else {
				reasons := map[string][]string{}
				for _, entry := range entries {
					reasons[entry.SessionUID] = entry.Reasons
				}
				for _, item := range page.Items {
					v.Rows = append(v.Rows, organizeRow{SessionMetadata: item, Selected: slices.Contains(v.Selected, item.UID), Reasons: reasons[item.UID]})
				}
			}
		}
	}
	if err != nil && v.Problem == "" {
		v.Problem = organizationProblem(err)
		status = http.StatusBadRequest
	}
	if v.Plan != nil && v.PlanJSON == "" {
		raw, err := json.Marshal(v.Plan)
		if err != nil {
			fail(w, r, err)
			return
		}
		v.PlanJSON = string(raw)
	}
	renderStatus(w, r, pageData{Title: "Organize sessions", View: "organize", Organize: v, AsOf: stampOf(s.now())}, status)
}

func organizationProblem(err error) string {
	switch {
	case errors.Is(err, catalog.ErrMigrationStale):
		return "The reviewed state changed. Nothing was applied. Your exact selection is retained; create and review a fresh preview."
	case errors.Is(err, catalog.ErrMigrationExpired):
		return "This preview expired after 24 hours. Your selection is retained; create and review a fresh preview."
	case errors.Is(err, catalog.ErrMigrationBlocked):
		return "Held, flagged or invalid sessions block this batch. Resolve them explicitly or exclude them, then preview again."
	case errors.Is(err, catalog.ErrBaySelection):
		return "A selected bay is unavailable. Check the current names and destinations, then preview again."
	case errors.Is(err, catalog.ErrSelectionLimit), errors.Is(err, catalog.ErrMigrationSize):
		return "This complete batch exceeds 500 sessions or an 8 MiB plan. Narrow the selection or use explicit smaller batches; no sample was selected."
	case errors.Is(err, catalog.ErrPage):
		return "The filters or page context changed. Your selection is retained. Choose Browse to restart paging with these filters."
	case errors.Is(err, catalog.ErrSelection):
		return "Choose exact sessions or deliberate metadata filters. Keep IDs valid and use exclusions to refine the set."
	case errors.Is(err, errPerSessionCohort):
		return "This per-session plan has no retained edits for a newly selected session. Narrow the selection to its original sessions or start a separate operation."
	case errors.Is(err, catalog.ErrSameBay):
		return "A destination also names a removed source. Choose separate sources and destinations."
	case errors.Is(err, catalog.ErrMigrationAuthority):
		return "A current administrator and a complete preview are required. Import the exact request and preview it here before applying."
	case errors.Is(err, baydoctor.ErrPolicy):
		return "Check the doctor policy JSON: use explicit directory/hash mappings, destinations and reasons. No purpose or bay names are guessed."
	case errors.Is(err, catalog.ErrMigrationInput), errors.Is(err, catalog.ErrBayName), errors.Is(err, catalog.ErrRuleEmpty):
		return "Check the operation, destination names and exact selection. New names use lowercase letters, digits and dashes; every proposed name must be a destination."
	default:
		return "The operation could not complete. Your selection is retained; operator logs have the details."
	}
}

func readOrganizationPlan(raw string) (catalog.BayMigrationPlan, error) {
	var plan catalog.BayMigrationPlan
	if len(raw) > 8<<20 {
		return plan, catalog.ErrMigrationSize
	}
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&plan) != nil || d.Decode(new(any)) != io.EOF {
		return plan, catalog.ErrMigrationInput
	}
	return plan, nil
}

func (s *Server) organizePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOrganizeForm)
	if r.ParseForm() != nil || !s.auth.CheckWrite(r, r.PostForm.Get("csrf")) {
		renderStatus(w, r, pageData{Title: "Request refused", View: "refused"}, http.StatusForbidden)
		return
	}
	f, err := parseOrganizeForm(r.PostForm)
	v := organizeView{Form: f}
	problem := func(err error) {
		v.Problem = organizationProblem(err)
		s.renderOrganize(w, r, v, http.StatusBadRequest)
	}
	if err != nil {
		problem(err)
		return
	}
	action := r.PostForm.Get("action")
	ctx, cancel := readContext(r)
	defer cancel()
	who, _ := webauth.Current(r)
	restore := func(req catalog.BayMigrationRequest) bool {
		var err error
		v.Form, err = s.formFromMigration(ctx, f, req)
		if err != nil {
			problem(err)
			return false
		}
		return true
	}
	switch {
	case action == "doctor":
		policy, err := baydoctor.DecodePolicy([]byte(f.DoctorPolicy))
		if err != nil {
			problem(err)
			return
		}
		scope, err := s.organizationScope(ctx, f)
		if err != nil {
			problem(err)
			return
		}
		request := baydoctor.Request{Policy: policy}
		if ids := splitSessionText(f.Selected); len(ids) > 0 {
			for _, uid := range ids {
				if !slices.Contains(splitSessionText(f.Exclude), uid) {
					request.UIDs = append(request.UIDs, uid)
				}
			}
			if len(request.UIDs) == 0 {
				problem(catalog.ErrSelection)
				return
			}
		} else {
			filter := f.Filter
			request.Filter = &filter
		}
		report, err := baydoctor.Recommend(ctx, s.catalog, scope, request, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Doctor = makeBayDoctorView(report)
	case action == "doctor-preview":
		var req catalog.BayMigrationRequest
		d := json.NewDecoder(strings.NewReader(r.PostForm.Get("request")))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF {
			problem(catalog.ErrMigrationInput)
			return
		}
		plan, err := s.catalog.PlanBayMigration(ctx, catalog.AllBays(), true, req, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Plan = &plan
		if !restore(plan.Request) {
			return
		}
	case action == "browse":
		v.Form.Cursor, v.Form.History, v.Form.Focus = "", nil, ""
	case r.PostForm.Has("next"):
		v.Form.History = append(v.Form.History, "~"+v.Form.Cursor)
		v.Form.Cursor = r.PostForm.Get("next")
	case action == "previous":
		if n := len(v.Form.History); n > 0 {
			v.Form.Cursor = strings.TrimPrefix(v.Form.History[n-1], "~")
			v.Form.History = v.Form.History[:n-1]
		}
	case r.PostForm.Has("related"):
		v.Form.Filter = catalog.DiscoveryRequest{CWDHash: r.PostForm.Get("related")}
		v.Form.Bays = ""
		v.Form.Focus = ""
		v.Form.Cursor = ""
		v.Form.History = nil
	case action == "all":
		scope, err := s.organizationScope(ctx, f)
		if err != nil {
			problem(err)
			return
		}
		// Clicking All filtered explicitly authorizes enumeration even with
		// no metadata field set. The same 500-candidate bound still applies.
		if f.Filter == (catalog.DiscoveryRequest{}) && f.Bays == "" && f.Focus == "" {
			ids := []string{}
			bays, err := s.catalog.Bays(ctx)
			if err != nil {
				problem(err)
				return
			}
			for _, b := range bays {
				ids = append(ids, b.ID)
			}
			scope = scope.SelectBays(ids)
		}
		var selection catalog.SessionSelection
		if f.Focus != "" {
			selection, err = s.catalog.SelectSessions(ctx, scope, catalog.SessionSelectionRequest{UIDs: []string{f.Focus}, Exclude: splitSessionText(f.Exclude)})
		} else {
			filter := f.Filter
			selection, err = s.catalog.SelectSessions(ctx, scope, catalog.SessionSelectionRequest{Filter: &filter, Exclude: splitSessionText(f.Exclude)})
		}
		if err != nil {
			problem(err)
			return
		}
		v.Form.Selected = strings.Join(selection.UIDs, "\n")
		v.Done = fmt.Sprintf("Frozen complete filtered selection: %d sessions. Related sessions require explicit inclusion.", len(selection.UIDs))
	case action == "preview":
		req, err := migrationRequestFromForm(f)
		if err != nil {
			problem(err)
			return
		}
		plan, err := s.catalog.PlanBayMigration(ctx, catalog.AllBays(), true, req, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Plan = &plan
	case action == "import" || action == "repreview":
		plan, err := readOrganizationPlan(r.PostForm.Get("plan"))
		if err != nil {
			problem(err)
			return
		}
		v.Plan = &plan
		if !restore(plan.Request) {
			return
		}
		fresh, err := s.catalog.PlanBayMigration(ctx, catalog.AllBays(), true, plan.Request, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Plan = &fresh
		if !restore(fresh.Request) {
			return
		}
	case action == "download":
		plan, err := readOrganizationPlan(r.PostForm.Get("plan"))
		if err != nil {
			problem(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="bay-plan.json"`)
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(plan)
		return
	case action == "apply":
		plan, err := readOrganizationPlan(r.PostForm.Get("plan"))
		if err != nil {
			problem(err)
			return
		}
		v.Plan, v.PlanJSON = &plan, r.PostForm.Get("plan")
		if !restore(plan.Request) {
			return
		}
		if !webauth.Fresh(r, s.now()) {
			v.Problem = "Sign in again before applying. This reviewed plan and exact selection are retained below; export it to resume later."
			s.renderOrganize(w, r, v, http.StatusForbidden)
			return
		}
		if r.PostForm.Get("confirm") != "true" {
			v.Problem = "Confirm the complete reviewed membership changes before applying."
			s.renderOrganize(w, r, v, http.StatusBadRequest)
			return
		}
		receipt, err := s.catalog.ApplyBayMigration(ctx, catalog.AllBays(), who.Admin, plan, r.PostForm.Get("create_proposed") == "true", actor(who).Audit, catalog.ViaWeb, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Receipt = &receipt
		v.Plan = nil
		v.PlanJSON = ""
		v.Done = "The exact reviewed batch committed. Verify its current membership below."
		if err := s.catalog.FlushAudit(ctx, s.reg.Lake().Dir); err != nil {
			v.Problem = "The migration committed; its audit remains queued. Verify the receipt before retrying."
		}
	case action == "recover":
		plan, err := s.catalog.RecoverBayMigration(ctx, catalog.AllBays(), true, r.PostForm.Get("receipt"), s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Plan = &plan
		if !restore(plan.Request) {
			return
		}
	case action == "release":
		if !webauth.Fresh(r, s.now()) || r.PostForm.Get("confirm_release") != "true" {
			v.Problem = "Release requires a recent sign-in and explicit confirmation that selected holds will place their requested bays."
			s.renderOrganize(w, r, v, http.StatusForbidden)
			return
		}
		ids := uniqueIDs(splitSessionText(f.Selected))
		if len(ids) == 0 {
			problem(catalog.ErrSelection)
			return
		}
		for _, uid := range ids {
			if slices.Contains(splitSessionText(f.Exclude), uid) {
				continue
			}
			err := s.catalog.ReleaseHold(ctx, uid, actor(who).Audit, catalog.ViaWeb, s.now())
			result := uid + ": released using current requests and routing"
			if err != nil {
				if errors.Is(err, catalog.ErrNoHold) {
					result = uid + ": no active hold; unchanged"
				} else {
					result = uid + ": release failed; unchanged; inspect operator logs"
					s.logError(r, "bulk hold release failed", err)
				}
			}
			v.ReleaseResults = append(v.ReleaseResults, result)
		}
		if err := s.catalog.FlushAudit(ctx, s.reg.Lake().Dir); err != nil {
			v.Problem = "Some releases may have committed; audit remains queued. Inspect the per-session results."
		}
	case action == "rule":
		if f.Filter.Project != "" || f.Filter.Unlinked || f.Filter.Machine != "" || f.Filter.UpdatedSince != "" || f.Filter.UpdatedUntil != "" {
			v.Problem = "Future routing uses directory/hash and harness metadata. Project IDs, unlinked, machine, time and exact UID selections are historical filters; remove them before this separate preview."
			s.renderOrganize(w, r, v, http.StatusBadRequest)
			return
		}
		req := catalog.BayRulePreviewRequest{Match: config.ProjectMatch{CWDPrefix: f.Filter.CWDPrefix, CWDHash: f.Filter.CWDHash}, Harness: f.Filter.Harness, Action: f.RuleAction, Bay: f.RuleBay, Proposed: f.RuleProposed}
		preview, err := s.catalog.PreviewBayRule(ctx, catalog.AllBays(), req, s.now())
		if err != nil {
			problem(err)
			return
		}
		v.Rule = &preview
		v.RuleCommand = ruleHostCommand(preview.Request, s.reg.Lake().Dir)
	default:
		problem(catalog.ErrMigrationInput)
		return
	}
	s.renderOrganize(w, r, v, http.StatusOK)
}

func (s *Server) formFromMigration(ctx context.Context, f organizeForm, req catalog.BayMigrationRequest) (organizeForm, error) {
	f.Selected = strings.Join(req.UIDs, "\n")
	if len(req.Edits) > 0 {
		ids := []string{}
		for _, edit := range req.Edits {
			ids = append(ids, edit.UID)
		}
		f.Selected = strings.Join(ids, "\n")
	}
	f.PerSession = nil
	f.Exclude, f.Add, f.Remove, f.Proposed, f.Reason = strings.Join(req.Exclude, "\n"), req.Add, req.Remove, strings.Join(req.Proposed, ","), req.Reason
	if len(req.Edits) > 0 {
		f.PerSession = &req
		f.Mode = "per-session"
		return f, nil
	}
	f.Mode = "add"
	if len(req.Remove) > 0 {
		f.Mode = "move"
	}
	// Existing-bay controls submit IDs. Preserve intent from imported names
	// and aliases by resolving them before checking those controls. Proposed
	// destinations remain names in their separate input, never checkboxes.
	resolve := func(refs []string, proposed []string) ([]string, error) {
		ids := []string{}
		for _, ref := range refs {
			if slices.Contains(proposed, ref) {
				continue
			}
			bay, err := s.catalog.ResolveBay(ctx, ref)
			if errors.Is(err, catalog.ErrNoBay) {
				// A stale reference stays selected as an unavailable control.
				// Do not discard intent or prevent an idempotent apply retry.
				ids = append(ids, ref)
				continue
			}
			if err != nil {
				return nil, err
			}
			ids = append(ids, bay.ID)
		}
		return uniqueIDs(ids), nil
	}
	add, err := resolve(req.Add, req.Proposed)
	if err != nil {
		return f, err
	}
	remove, err := resolve(req.Remove, nil)
	if err != nil {
		return f, err
	}
	f.Add, f.Remove = add, remove
	return f, nil
}

func ruleHostCommand(req catalog.BayRulePreviewRequest, data string) string {
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	command := "terva-lampi serve bays rule " + quote(req.Bay) + " --" + req.Action + " --data " + quote(data)
	for _, field := range []struct{ flag, value string }{{"cwd-prefix", req.Match.CWDPrefix}, {"cwd-hash", req.Match.CWDHash}, {"cwd-glob", req.Match.CWDGlob}, {"git-remote", req.Match.GitRemote}, {"git-remote-prefix", req.Match.GitRemotePrefix}, {"harness", req.Harness}} {
		if field.value != "" {
			command += " --" + field.flag + " " + quote(field.value)
		}
	}
	return command
}

func (s *Server) organizeReceipt(w http.ResponseWriter, r *http.Request) {
	check, err := s.catalog.VerifyBayMigration(r.Context(), catalog.AllBays(), true, r.PathValue("id"))
	if err != nil {
		fail(w, r, err)
		return
	}
	v := organizeView{Form: initialOrganizeForm(), Verification: &check}
	ids := []string{}
	for _, item := range check.Items {
		ids = append(ids, item.UID)
	}
	v.Form.Selected = strings.Join(ids, "\n")
	s.renderOrganize(w, r, v, http.StatusOK)
}

func (f organizeForm) HistoryText() string { return strings.Join(f.History, "\n") }

func (f organizeForm) PerSessionJSON() string {
	if f.PerSession == nil {
		return ""
	}
	raw, _ := json.Marshal(f.PerSession)
	return string(raw)
}

func (v organizeView) BayLabel(ref string) string {
	for _, bay := range v.Bays {
		if bay.ID == ref {
			return bay.Name
		}
	}
	return ref
}
