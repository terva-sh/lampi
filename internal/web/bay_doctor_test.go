package web

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func seedDoctorSession(t *testing.T, c *catalog.Catalog, native, cwd string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "claude", NativeSessionID: native, Project: protocol.Project{CWD: cwd}, Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "session.jsonl", SHA256: strings.Repeat("b", 64), Size: 12}}}
	a, err := c.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return a.SessionUID
}
func doctorPolicy() baydoctor.Policy {
	return baydoctor.Policy{Mappings: []baydoctor.Mapping{{ID: "review-layout", Match: config.ProjectMatch{CWDPrefix: "/review-runs"}, Harness: "claude", Destinations: []string{"code-reviews", "learning"}, Proposed: []string{"code-reviews", "learning"}, Reason: "Verified fixture layout; inspect purpose"}}}
}

func TestBayDoctorMCPHTTPDashboardAndScope(t *testing.T) {
	lake, idp, h, _, _ := mcpLake(t)
	ctx := t.Context()
	now := time.Now()
	reviewer := seedDoctorSession(t, lake.Catalog, "reviewer", "/review-runs/one")
	rater := seedDoctorSession(t, lake.Catalog, "finding-rater", "/review-runs/one")
	dev := seedDoctorSession(t, lake.Catalog, "development", "/repo/project")
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "doctor", "", catalog.PermEventsRead)
	req := baydoctor.Request{Policy: doctorPolicy()}
	rawArgs, _ := json.Marshal(req)
	var args map[string]any
	_ = json.Unmarshal(rawArgs, &args)
	text, bad := callTool(t, h, token, "recommend_bays", args)
	w := postPreview(h, "/api/read/v1/recommend-bays", token, req)
	var a, b baydoctor.Report
	if bad || w.Code != 200 || json.Unmarshal([]byte(text), &a) != nil || json.Unmarshal(w.Body.Bytes(), &b) != nil {
		t.Fatal("doctor", text, w.Code, w.Body.String())
	}
	a.AsOf = b.AsOf
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) || len(a.Findings) != 2 || !a.Complete {
		t.Fatal("parity", string(aa), string(bb))
	}
	for _, f := range a.Findings {
		if f.UID == dev || f.Preview == nil || len(f.Preview.Add) != 2 || f.Uncertainty == "" {
			t.Fatal("bad finding", f)
		}
	}
	if len(a.Findings[0].Current) != 1 || a.Findings[0].Current[0].Name != "default" {
		t.Fatal("current membership")
	}
	bays, _ := lake.Catalog.Bays(ctx)
	if len(bays) != 1 {
		t.Fatal("recommendation created bays")
	}
	rules, _ := lake.Catalog.BayRules(ctx)
	if len(rules) != 0 {
		t.Fatal("recommendation installed rules")
	}
	policyJSON, _ := json.Marshal(req.Policy)
	form := organizationForm(t, get(h, organizePath, admin).Body.String(), "organize-form")
	form.Set("doctor_policy", string(policyJSON))
	form.Set("action", "doctor")
	page := postForm(h, organizePath, form, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Bay doctor report") || !strings.Contains(page.Body.String(), reviewer) || !strings.Contains(page.Body.String(), rater) {
		t.Fatal("dashboard", page.Code, page.Body.String())
	}
	previewRequest := stdhtml.UnescapeString(between(page.Body.String(), `name="request" value="`, `"`))
	page = postForm(h, organizePath, url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"doctor-preview"}, "request": {previewRequest}}, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "complete exact plan") || !strings.Contains(page.Body.String(), "proposed") {
		t.Fatal("preview handoff", page.Code, page.Body.String())
	}
	visible, _ := lake.Catalog.CreateBay(ctx, "visible", "test", now)
	hidden, _ := lake.Catalog.CreateBay(ctx, "secret-membership-sentinel", "test", now)
	for _, uid := range []string{reviewer, rater} {
		_, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: hidden.ID, Actor: "test", Via: catalog.ViaCLI}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _ = lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: reviewer, Bay: visible.ID, Actor: "test", Via: catalog.ViaCLI}, now)
	scoped := ReadTokenPrefix + "synthetic-doctor-scope"
	_, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "scope", Permissions: []string{catalog.PermEventsRead}, BayScoped: true, Bays: []string{visible.ID}, CreatedBy: "test", Expires: now.Add(time.Hour)}, hashReadToken(scoped), now)
	if err != nil {
		t.Fatal(err)
	}
	w = postPreview(h, "/api/read/v1/recommend-bays", scoped, req)
	if w.Code != 200 || strings.Contains(w.Body.String(), rater) || strings.Contains(w.Body.String(), hidden.ID) || strings.Contains(w.Body.String(), hidden.Name) || strings.Contains(w.Body.String(), "bay_default") {
		t.Fatal("scope leak", w.Code, w.Body.String())
	}
	req.Policy.Mappings[0].Destinations = []string{hidden.Name}
	req.Policy.Mappings[0].Proposed = nil
	w = postPreview(h, "/api/read/v1/recommend-bays", scoped, req)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "unknown_or_inaccessible_destination") || strings.Contains(w.Body.String(), "preview_request") {
		t.Fatal("destination leak", w.Body.String())
	}
}
func TestBayDoctorInvalidAuthAndIncompleteReport(t *testing.T) {
	lake, _, h, _, _ := mcpLake(t)
	if w := postPreview(h, "/api/read/v1/recommend-bays", "", baydoctor.Request{Policy: doctorPolicy()}); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	for i := 0; i < 501; i++ {
		seedDoctorSession(t, lake.Catalog, fmt.Sprintf("run-%03d", i), "/review-runs/one")
	}
	report, err := baydoctor.Recommend(t.Context(), lake.Catalog, catalog.AllBays(), baydoctor.Request{Policy: doctorPolicy()}, time.Now())
	if err != nil || report.Complete || report.Inspected != 500 || len(report.Findings) != 500 {
		t.Fatal("bounded", err, report.Inspected, len(report.Findings))
	}
	for _, f := range report.Findings {
		if f.Preview != nil || f.Status != "incomplete_inventory" {
			t.Fatal("incomplete handoff")
		}
	}
	// The strict common decoder refuses unsupported evidence and mutations.
	for _, raw := range []string{`{"policy":{"mappings":[]},"write":true}`, `{"policy":null}`, `{"policy":{"mappings":[]},"filter":{"limit":1}}`, `{"policy":{"mappings":[],"typo":true}}`} {
		if _, err := (&Server{catalog: lake.Catalog}).recommendCall(t.Context(), catalog.AllBays(), json.RawMessage(raw)); err == nil {
			t.Fatal("invalid accepted", raw)
		}
	}
}

func TestBayDoctorExactCohortKeepsDestinationAuthority(t *testing.T) {
	lake, idp, h, _, _ := mcpLake(t)
	ctx, now := t.Context(), time.Now()
	reviewer := seedDoctorSession(t, lake.Catalog, "selected-reviewer", "/review-runs/one")
	other := seedDoctorSession(t, lake.Catalog, "other-reviewer", "/review-runs/two")
	destination, err := lake.Catalog.CreateBay(ctx, "existing-reviews", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: other, Bay: destination.ID, Actor: "test", Via: catalog.ViaCLI}, now)
	if err != nil {
		t.Fatal(err)
	}
	policy := doctorPolicy()
	policy.Mappings[0].Destinations = []string{destination.Name}
	policy.Mappings[0].Proposed = nil
	admin := signInAs(t, idp, h, "owners")
	unrestricted, _ := mintReadTokenWith(t, h, admin, "doctor-existing", "", catalog.PermEventsRead)
	fixed := ReadTokenPrefix + "synthetic-doctor-fixed"
	_, err = lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "fixed", Permissions: []string{catalog.PermEventsRead}, Sessions: []string{reviewer}, CreatedBy: "test", Expires: now.Add(time.Hour)}, hashReadToken(fixed), now)
	if err != nil {
		t.Fatal(err)
	}
	scoped := ReadTokenPrefix + "synthetic-doctor-authorized-bays"
	sourceOnly := ReadTokenPrefix + "synthetic-doctor-source-only"
	fixedWithDestination := ReadTokenPrefix + "synthetic-doctor-fixed-with-destination"
	for _, tok := range []struct {
		secret   string
		bays     []string
		sessions []string
	}{
		{scoped, []string{catalog.DefaultBayID, destination.ID}, nil},
		{sourceOnly, []string{catalog.DefaultBayID}, nil},
		{fixedWithDestination, nil, []string{reviewer, other}},
	} {
		_, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "doctor-destination-scope", Permissions: []string{catalog.PermEventsRead}, Sessions: tok.sessions, BayScoped: tok.bays != nil, Bays: tok.bays, CreatedBy: "test", Expires: now.Add(time.Hour)}, hashReadToken(tok.secret), now)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name, token string
		wantPreview bool
	}{
		{"unrestricted", unrestricted, true},
		{"authorized-bays", scoped, true},
		{"source-only", sourceOnly, false},
		{"fixed-session", fixed, false},
		{"fixed-with-destination", fixedWithDestination, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := baydoctor.Request{Policy: policy, UIDs: []string{reviewer}, Bays: "default"}
			raw, _ := json.Marshal(req)
			var args map[string]any
			_ = json.Unmarshal(raw, &args)
			text, bad := callTool(t, h, tc.token, "recommend_bays", args)
			w := postPreview(h, "/api/read/v1/recommend-bays", tc.token, req)
			for _, body := range []string{text, w.Body.String()} {
				var report baydoctor.Report
				if bad || w.Code != 200 || json.Unmarshal([]byte(body), &report) != nil || report.Inspected != 1 || len(report.Findings) != 1 {
					t.Fatal("exact report", bad, w.Code, body)
				}
				f := report.Findings[0]
				if f.UID != reviewer || (f.Preview != nil) != tc.wantPreview {
					t.Fatal("destination authorization", f)
				}
				if tc.wantPreview {
					if f.Status != "suggestion" || len(f.Preview.Add) != 1 || f.Preview.Add[0] != destination.ID {
						t.Fatal("missing destination", f)
					}
				} else if f.Status != "unknown_or_inaccessible_destination" || strings.Contains(body, destination.ID) || strings.Contains(body, other) {
					t.Fatal("fixed-session destination leak", body)
				}
			}
		})
	}
	rawPolicy, _ := json.Marshal(policy)
	for _, mode := range []string{"exact", "focus-exact", "focus-filter", "per-session-exact", "per-session-filter", "empty-focus-filter"} {
		t.Run("dashboard-"+mode, func(t *testing.T) {
			form := organizationForm(t, get(h, organizePath, admin).Body.String(), "organize-form")
			form.Set("doctor_policy", string(rawPolicy))
			form.Set("bays", "default")
			form.Set("selected", reviewer)
			if strings.HasPrefix(mode, "focus") || strings.HasPrefix(mode, "empty-focus") {
				form.Set("focus", reviewer)
			}
			if strings.HasPrefix(mode, "per-session") {
				retained, _ := json.Marshal(catalog.BayMigrationRequest{Edits: []catalog.MigrationEdit{{UID: reviewer, Add: []string{catalog.DefaultBayID}}}})
				form.Set("mode", "per-session")
				form.Set("per_session_request", string(retained))
			}
			if strings.HasSuffix(mode, "filter") {
				form.Set("selected", "")
				form.Set("harness", "claude")
			}
			if mode == "empty-focus-filter" {
				form.Set("cwd_prefix", "/no-matching-reviewers")
			}
			form.Set("action", "doctor")
			page := postForm(h, organizePath, form, admin)
			if mode == "empty-focus-filter" {
				if page.Code != 200 || !strings.Contains(page.Body.String(), "0 inspected") || strings.Contains(page.Body.String(), `name="request"`) {
					t.Fatal("empty cohort expanded", page.Code, page.Body.String())
				}
				return
			}
			preview := stdhtml.UnescapeString(between(page.Body.String(), `name="request" value="`, `"`))
			var req catalog.BayMigrationRequest
			if page.Code != 200 || json.Unmarshal([]byte(preview), &req) != nil || len(req.UIDs) != 1 || req.UIDs[0] != reviewer || len(req.Add) != 1 || req.Add[0] != destination.ID {
				t.Fatal("dashboard destination or cohort", page.Code, preview, page.Body.String())
			}
		})
	}
	w := postPreview(h, "/api/read/v1/recommend-bays", unrestricted, baydoctor.Request{Policy: policy, UIDs: []string{}})
	var empty baydoctor.Report
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &empty) != nil || empty.Inspected != 0 || len(empty.Findings) != 0 || !empty.Complete {
		t.Fatal("empty explicit cohort", w.Code, w.Body.String())
	}
	w = postPreview(h, "/api/read/v1/recommend-bays", fixed, baydoctor.Request{Policy: policy, UIDs: []string{reviewer, other}})
	var restricted baydoctor.Report
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &restricted) != nil || restricted.Inspected != 1 || strings.Contains(w.Body.String(), other) {
		t.Fatal("request broadened credential authority", w.Code, w.Body.String())
	}
}
