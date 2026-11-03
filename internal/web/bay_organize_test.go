package web

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

// Read the real rendered form, including textareas and checked controls, to
// exercise selection round trips without coupling tests to hidden fields.
func organizationForm(t *testing.T, body, id string) url.Values {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{}
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, inside bool) {
		attrs := map[string]string{}
		for _, a := range n.Attr {
			attrs[a.Key] = a.Val
		}
		if n.Data == "form" {
			inside = attrs["id"] == id
		}
		if inside && attrs["name"] != "" {
			name := attrs["name"]
			switch n.Data {
			case "input":
				typ := attrs["type"]
				_, checked := attrs["checked"]
				if typ != "submit" && (typ != "checkbox" && typ != "radio" || checked) {
					values.Add(name, attrs["value"])
				}
			case "textarea":
				var b strings.Builder
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					b.WriteString(c.Data)
				}
				values.Add(name, b.String())
			case "select":
				first := true
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Data != "option" {
						continue
					}
					a := map[string]string{}
					for _, v := range c.Attr {
						a[v.Key] = v.Val
					}
					if _, ok := a["selected"]; first || ok {
						values.Set(name, a["value"])
					}
					first = false
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, inside)
		}
	}
	walk(doc, false)
	if len(values) == 0 {
		t.Fatalf("form %s missing", id)
	}
	return values
}

func TestOrganizeSelectionPagingAndRelated(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	var ids []string
	for i := 0; i < 53; i++ {
		ids = append(ids, ingestHead(t, lake, fmt.Sprintf("run-%02d", i), strings.Repeat("ab", 32), 4))
	}
	admin := signInAs(t, idp, h, "owners")
	page := get(h, organizePath, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	body := page.Body.String()
	for _, want := range []string{`<main id="main" class="wide">`, `<caption>`, `scope="col"`, `role="status"`, `aria-describedby="selection-help"`, `name="mode"`, `name="exclude"`} {
		if !strings.Contains(body, want) {
			t.Error("missing accessible control", want)
		}
	}
	f := organizationForm(t, body, "organize-form")
	if len(f["page_uid"]) != 50 {
		t.Fatal("first page", len(f["page_uid"]))
	}
	selected := f["page_uid"][0]
	f.Set("selected_page", selected)
	next := between(body, `name="next" value="`, `"`)
	f.Set("next", next)
	page = postForm(h, organizePath, f, admin)
	f = organizationForm(t, page.Body.String(), "organize-form")
	if f.Get("selected") != selected || len(f["page_uid"]) != 3 {
		t.Fatal("selection lost on next page", f)
	}
	other := f["page_uid"][0]
	f.Set("selected_page", other)
	f.Set("action", "previous")
	page = postForm(h, organizePath, f, admin)
	f = organizationForm(t, page.Body.String(), "organize-form")
	if !strings.Contains(f.Get("selected"), selected) || !strings.Contains(f.Get("selected"), other) {
		t.Fatal("selection lost on previous page")
	}
	f.Set("action", "preview") // invalid destination retains exact selection
	page = postForm(h, organizePath, f, admin)
	if page.Code != 400 || !strings.Contains(page.Body.String(), other) {
		t.Fatal("error lost selection", page.Code)
	}
	f = organizationForm(t, page.Body.String(), "organize-form")
	f.Set("action", "all")
	f.Set("exclude", ids[0])
	page = postForm(h, organizePath, f, admin)
	f = organizationForm(t, page.Body.String(), "organize-form")
	if len(splitSessionText(f.Get("selected"))) != 52 || strings.Contains(f.Get("selected"), ids[0]) {
		t.Fatal("all filtered selection", f.Get("selected"))
	}
	// A related inspection clears project and bay restrictions, retaining UIDs.
	f.Set("related", "shared-hash")
	f.Set("project", "some-project")
	f.Del("action")
	page = postForm(h, organizePath, f, admin)
	f = organizationForm(t, page.Body.String(), "organize-form")
	if f.Get("project") != "" || f.Get("bays") != "" || f.Get("cwd_hash") != "shared-hash" || len(splitSessionText(f.Get("selected"))) != 52 {
		t.Fatal("related inspection lost state")
	}
}

func TestOrganizePreviewApplyVerifyCorrection(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	now := time.Now()
	uid := ingestHead(t, lake, "reviewer", strings.Repeat("ab", 32), 4)
	project, _ := lake.Catalog.CreateBay(ctx, "project-a", "test", now)
	_, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: project.ID, Actor: "test", Via: catalog.ViaCLI}, now)
	if err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	for _, path := range []string{adminBaysPath, "/sessions/" + uid} {
		if !strings.Contains(get(h, path, admin).Body.String(), organizePath) {
			t.Fatal("no organization entry", path)
		}
	}
	f := organizationForm(t, get(h, organizePath+"?uid="+uid, admin).Body.String(), "organize-form")
	f.Set("mode", "move")
	f.Set("remove", catalog.DefaultBayID)
	f.Set("proposed", "code-reviews,learning")
	f.Set("action", "preview")
	page := postForm(h, organizePath, f, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	planForm := organizationForm(t, page.Body.String(), "plan-form")
	var plan catalog.BayMigrationPlan
	if json.Unmarshal([]byte(planForm.Get("plan")), &plan) != nil || plan.Changed != 1 || len(plan.Items[0].After) != 3 {
		t.Fatal("preview", planForm)
	}
	if _, err := lake.Catalog.ResolveBay(ctx, "code-reviews"); err == nil {
		t.Fatal("preview created bay")
	}
	planForm.Set("action", "apply")
	if w := postForm(h, organizePath, planForm, admin); w.Code != 400 {
		t.Fatal("unconfirmed apply", w.Code)
	}
	planForm.Set("confirm", "true")
	planForm.Set("create_proposed", "true")
	page = postForm(h, organizePath, planForm, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Migration receipt") {
		t.Fatal("apply", page.Code, page.Body.String())
	}
	page = postForm(h, organizePath, planForm, admin)
	if !strings.Contains(page.Body.String(), "already applied") {
		t.Fatal("retry replayed")
	}
	page = get(h, organizePath+"/receipts/"+plan.ID, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Every membership matches") {
		t.Fatal("verify", page.Code)
	}
	f = url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"recover"}, "receipt": {plan.ID}}
	page = postForm(h, organizePath, f, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "complete exact plan") {
		t.Fatal("recovery", page.Code, page.Body.String())
	}
	// A membership change invalidates a fresh plan without applying any edits.
	fresh, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, catalog.BayMigrationRequest{UIDs: []string{uid}, Add: []string{"default"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fresh)
	_, err = lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: "default", Actor: "later", Via: catalog.ViaCLI}, now)
	if err != nil {
		t.Fatal(err)
	}
	f = url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"apply"}, "plan": {string(raw)}, "confirm": {"true"}}
	page = postForm(h, organizePath, f, admin)
	if page.Code != 400 || !strings.Contains(page.Body.String(), "Nothing was applied") || !strings.Contains(page.Body.String(), uid) {
		t.Fatal("stale handling", page.Code)
	}
}

func TestOrganizeRestoresReferencedBaysBeforeSelectionRefinement(t *testing.T) {
	for _, tc := range []struct {
		name, action, mode string
		alias              bool
	}{
		{"import-add-names", "import", "add", false},
		{"import-move-aliases", "import", "move", true},
		{"doctor-move-names", "doctor-preview", "move", false},
		{"import-add-aliases", "import", "add", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lake, idp, h, _ := rawLake(t, nil)
			ctx, now := t.Context(), time.Now()
			one := ingestHead(t, lake, "reviewer", strings.Repeat("ab", 32), 4)
			two := ingestHead(t, lake, "companion", strings.Repeat("cd", 32), 4)
			dst, err := lake.Catalog.CreateBay(ctx, "code-reviews", "test", now)
			if err != nil {
				t.Fatal(err)
			}
			learning, err := lake.Catalog.CreateBay(ctx, "learning", "test", now)
			if err != nil {
				t.Fatal(err)
			}
			destination, source := dst.Name, "default"
			if tc.alias {
				if err := lake.Catalog.AliasBay(ctx, dst.ID, "reviews-alias", "test", now); err != nil {
					t.Fatal(err)
				}
				if err := lake.Catalog.AliasBay(ctx, "default", "inbox-alias", "test", now); err != nil {
					t.Fatal(err)
				}
				destination, source = "reviews-alias", "inbox-alias"
			}
			req := catalog.BayMigrationRequest{UIDs: []string{one, two}, Add: []string{destination, learning.Name, "proposed-bay"}, Proposed: []string{"proposed-bay"}, Reason: "Reviewed cohort"}
			if tc.mode == "move" {
				req.Remove = []string{source}
			}
			plan, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), false, req, now)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(plan)
			admin := signInAs(t, idp, h, "owners")
			f := url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {tc.action}, "plan": {string(raw)}}
			if tc.action == "doctor-preview" {
				raw, _ = json.Marshal(req)
				f.Set("request", string(raw))
			}
			page := postForm(h, organizePath, f, admin)
			if page.Code != 200 {
				t.Fatal(page.Code, page.Body.String())
			}
			// Re-preview the imported request before changing the exact cohort.
			// Both restoration paths must preserve the checked controls.
			for _, repreview := range []bool{false, true} {
				if repreview {
					pf := organizationForm(t, page.Body.String(), "plan-form")
					pf.Set("action", "repreview")
					page = postForm(h, organizePath, pf, admin)
					if page.Code != 200 {
						t.Fatal(page.Code, page.Body.String())
					}
				}
				f = organizationForm(t, page.Body.String(), "organize-form")
				if !slices.Contains(f["add"], dst.ID) || !slices.Contains(f["add"], learning.ID) || len(f["add"]) != 2 || f.Get("proposed") != "proposed-bay" || f.Get("mode") != tc.mode {
					t.Fatalf("restored destinations lost intent: %v", f)
				}
				if tc.mode == "move" && (len(f["remove"]) != 1 || f.Get("remove") != catalog.DefaultBayID) {
					t.Fatalf("restored source lost intent: %v", f)
				}
			}
			f.Set("exclude", two)
			f.Set("action", "preview")
			page = postForm(h, organizePath, f, admin)
			if page.Code != 200 {
				t.Fatal(page.Code, page.Body.String())
			}
			pf := organizationForm(t, page.Body.String(), "plan-form")
			if err := json.Unmarshal([]byte(pf.Get("plan")), &plan); err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 1 || plan.Items[0].UID != one || !slices.Equal(plan.Request.Exclude, []string{two}) || len(plan.Items[0].Add) != 3 || plan.Rejected != 0 || !slices.Contains(plan.Items[0].Add, dst.ID) || !slices.Contains(plan.Items[0].Add, learning.ID) {
				t.Fatalf("refined plan lost destinations or selection: %+v", plan)
			}
			if tc.mode == "move" && !slices.Equal(plan.Items[0].Remove, []string{catalog.DefaultBayID}) || tc.mode == "add" && len(plan.Items[0].Remove) != 0 {
				t.Fatalf("refined operation changed source intent: %+v", plan)
			}
		})
	}
}

func TestOrganizeRetainsUnavailableDestinationAndAllowsReceiptRetry(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx, now := t.Context(), time.Now()
	uid := ingestHead(t, lake, "reviewer", strings.Repeat("ab", 32), 4)
	dst, err := lake.Catalog.CreateBay(ctx, "code-reviews", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, catalog.BayMigrationRequest{UIDs: []string{uid}, Add: []string{dst.Name}}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(plan)
	admin := signInAs(t, idp, h, "owners")
	f := url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"apply"}, "plan": {string(raw)}, "confirm": {"true"}}
	if page := postForm(h, organizePath, f, admin); page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	if _, err := lake.Catalog.DeleteBay(ctx, dst.ID, "test", now); err != nil {
		t.Fatal(err)
	}
	page := postForm(h, organizePath, f, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "already applied") {
		t.Fatal("form restoration prevented idempotent receipt retry", page.Code, page.Body.String())
	}
	f = organizationForm(t, page.Body.String(), "organize-form")
	if !slices.Equal(f["add"], []string{dst.Name}) || !strings.Contains(page.Body.String(), "Unavailable destination") {
		t.Fatal("unavailable destination was silently discarded", f)
	}
	f.Set("action", "preview")
	page = postForm(h, organizePath, f, admin)
	if page.Code != 400 || !strings.Contains(page.Body.String(), "selected bay is unavailable") {
		t.Fatal("unavailable intent was accepted as a different operation", page.Code, page.Body.String())
	}
}

func TestOrganizeRecoveryRetainsPerSessionEditsDuringRefinement(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx, now := t.Context(), time.Now()
	one := ingestHead(t, lake, "reviewer", strings.Repeat("ab", 32), 4)
	two := ingestHead(t, lake, "companion", strings.Repeat("cd", 32), 4)
	other := ingestHead(t, lake, "development", strings.Repeat("ef", 32), 4)
	reviews, err := lake.Catalog.CreateBay(ctx, "reviews", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	learning, err := lake.Catalog.CreateBay(ctx, "learning", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	original, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, catalog.BayMigrationRequest{Edits: []catalog.MigrationEdit{
		{UID: one, Add: []string{reviews.ID}, Remove: []string{"default"}},
		{UID: two, Add: []string{learning.ID}, Remove: []string{"default"}},
	}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.ApplyBayMigration(ctx, catalog.AllBays(), true, original, false, "test", catalog.ViaCLI, now); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	page := postForm(h, organizePath, url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"recover"}, "receipt": {original.ID}}, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Retained per-session membership edits") {
		t.Fatal(page.Code, page.Body.String())
	}
	body := page.Body.String()
	var correctionForm url.Values
	for _, refinement := range []string{"exclude", "selection", "filtered-selection", "new-session", "missing-request"} {
		t.Run(refinement, func(t *testing.T) {
			f := organizationForm(t, body, "organize-form")
			if f.Get("mode") != "per-session" || f.Get("per_session_request") == "" || len(f["add"])+len(f["remove"]) != 0 {
				t.Fatal("recovery rendered as uniform editor", f)
			}
			f.Set("action", "preview")
			switch refinement {
			case "exclude":
				f.Set("exclude", two)
			case "selection":
				f.Set("selected", one)
			case "filtered-selection":
				f.Set("focus", one)
				f.Set("action", "all")
				page := postForm(h, organizePath, f, admin)
				if page.Code != 200 {
					t.Fatal(page.Code, page.Body.String())
				}
				f = organizationForm(t, page.Body.String(), "organize-form")
				f.Set("action", "preview")
			case "new-session":
				f.Set("selected", one+"\n"+other)
			case "missing-request":
				f.Del("per_session_request")
			}
			page := postForm(h, organizePath, f, admin)
			if refinement == "new-session" || refinement == "missing-request" {
				if page.Code != 400 {
					t.Fatal("lossy or inferred per-session request accepted", page.Code, page.Body.String())
				}
				if refinement == "new-session" && !strings.Contains(page.Body.String(), "no retained edits") {
					t.Fatal("missing cohort explanation", page.Body.String())
				}
				return
			}
			if page.Code != 200 {
				t.Fatal(page.Code, page.Body.String())
			}
			pf := organizationForm(t, page.Body.String(), "plan-form")
			var plan catalog.BayMigrationPlan
			if err := json.Unmarshal([]byte(pf.Get("plan")), &plan); err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 1 || plan.Items[0].UID != one || !slices.Equal(plan.Items[0].Add, []string{catalog.DefaultBayID}) || !slices.Equal(plan.Items[0].Remove, []string{reviews.ID}) || plan.Request.RecoveryOf != original.ID || len(plan.Request.Add)+len(plan.Request.Remove)+len(plan.Request.UIDs) != 0 {
				t.Fatalf("refinement lost the inverse operation: %+v", plan)
			}
			if refinement == "selection" {
				correctionForm = pf
			}
			pf.Set("action", "repreview")
			page = postForm(h, organizePath, pf, admin)
			if page.Code != 200 || organizationForm(t, page.Body.String(), "organize-form").Get("mode") != "per-session" {
				t.Fatal("re-preview lost retained editor", page.Code)
			}
		})
	}
	for uid, expected := range map[string]string{one: reviews.ID, two: learning.ID, other: catalog.DefaultBayID} {
		bays, err := lake.Catalog.SessionBays(ctx, uid)
		if err != nil || !slices.Equal(bays, []string{expected}) {
			t.Fatalf("preview changed memberships: %s %v %v", uid, bays, err)
		}
	}
	correctionForm.Set("action", "apply")
	correctionForm.Set("confirm", "true")
	page = postForm(h, organizePath, correctionForm, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	for uid, expected := range map[string]string{one: catalog.DefaultBayID, two: learning.ID, other: catalog.DefaultBayID} {
		bays, err := lake.Catalog.SessionBays(ctx, uid)
		if err != nil || !slices.Equal(bays, []string{expected}) {
			t.Fatalf("refined correction affected wrong memberships: %s %v %v", uid, bays, err)
		}
	}
	stale := organizationForm(t, body, "plan-form")
	stale.Set("action", "repreview")
	page = postForm(h, organizePath, stale, admin)
	if page.Code != 400 || !strings.Contains(page.Body.String(), "reviewed state changed") || organizationForm(t, page.Body.String(), "organize-form").Get("per_session_request") == "" {
		t.Fatal("stale correction refresh bypassed receipt state or lost retained edits", page.Code, page.Body.String())
	}
}

func TestOrganizeImportedPerSessionEditsPruneUnusedProposals(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx, now := t.Context(), time.Now()
	one := ingestHead(t, lake, "reviewer", strings.Repeat("ab", 32), 4)
	two := ingestHead(t, lake, "companion", strings.Repeat("cd", 32), 4)
	dst, err := lake.Catalog.CreateBay(ctx, "reviews", "test", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.AliasBay(ctx, dst.ID, "review-alias", "test", now); err != nil {
		t.Fatal(err)
	}
	imported, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, catalog.BayMigrationRequest{Edits: []catalog.MigrationEdit{
		{UID: one, Add: []string{"review-alias"}, Remove: []string{"default"}},
		{UID: two, Add: []string{"proposed-bay"}},
	}, Proposed: []string{"proposed-bay"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(imported)
	admin := signInAs(t, idp, h, "owners")
	page := postForm(h, organizePath, url.Values{"csrf": {csrfOf(t, h, admin)}, "action": {"import"}, "plan": {string(raw)}}, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	f := organizationForm(t, page.Body.String(), "organize-form")
	f.Set("exclude", two)
	f.Set("action", "preview")
	page = postForm(h, organizePath, f, admin)
	if page.Code != 200 {
		t.Fatal(page.Code, page.Body.String())
	}
	var plan catalog.BayMigrationPlan
	pf := organizationForm(t, page.Body.String(), "plan-form")
	if err := json.Unmarshal([]byte(pf.Get("plan")), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].UID != one || !slices.Equal(plan.Items[0].Add, []string{dst.ID}) || !slices.Equal(plan.Items[0].Remove, []string{catalog.DefaultBayID}) || len(plan.Request.Proposed) != 0 {
		t.Fatalf("refined imported operation or proposals changed incorrectly: %+v", plan)
	}
}

func TestOrganizeAuthorityHoldsAndFutureRouting(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	now := time.Now()
	for _, group := range []string{"readers", "ops"} {
		c := signInAs(t, idp, h, group)
		if get(h, organizePath, c).Code != 404 || postForm(h, organizePath, url.Values{}, c).Code != 404 {
			t.Fatal("nonadmin organization")
		}
	}
	admin := signInAs(t, idp, h, "owners")
	if postForm(h, organizePath, url.Values{"csrf": {"wrong"}, "action": {"all"}}, admin).Code != 403 {
		t.Fatal("csrf")
	}
	_, _ = lake.Catalog.CreateBay(ctx, "review", "test", now)
	_, err := lake.Catalog.AddBayRule(ctx, catalog.BayRule{Match: config.ProjectMatch{CWDPrefix: "/held"}, Action: catalog.RuleHold, BayID: "review"}, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	uid := ingestHeld(t, lake, "held-run")
	f := organizationForm(t, get(h, organizePath+"?uid="+uid, admin).Body.String(), "organize-form")
	f.Set("add", catalog.DefaultBayID)
	f.Set("action", "preview")
	page := postForm(h, organizePath, f, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "held_or_flagged") || !strings.Contains(page.Body.String(), "held by hold rule") {
		t.Fatal("hold unexplained", page.Code)
	}
	f.Set("action", "release")
	if postForm(h, organizePath, f, admin).Code != 403 {
		t.Fatal("unconfirmed release")
	}
	f.Set("confirm_release", "true")
	page = postForm(h, organizePath, f, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "released using current requests") {
		t.Fatal("release", page.Code)
	}
	f.Set("action", "rule")
	f.Set("cwd_prefix", "/held")
	f.Set("rule_action", "add")
	f.Set("rule_bay", "review")
	page = postForm(h, organizePath, f, admin)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "No rule was installed") {
		t.Fatal("routing", page.Code, page.Body.String())
	}
	rules, _ := lake.Catalog.BayRules(ctx)
	if len(rules) != 1 {
		t.Fatal("preview installed rule")
	}
	// Old sign-ins can preview and export but retain their plan on refused apply.
	idp.AuthTime = time.Now().Add(-time.Hour)
	stale := signInAs(t, idp, h, "owners")
	plan, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, catalog.BayMigrationRequest{UIDs: []string{uid}, Add: []string{"review"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(plan)
	f = url.Values{"csrf": {csrfOf(t, h, stale)}, "action": {"apply"}, "plan": {string(raw)}, "confirm": {"true"}}
	page = postForm(h, organizePath, f, stale)
	if page.Code != 403 || !strings.Contains(page.Body.String(), uid) || !strings.Contains(page.Body.String(), "retained below") {
		t.Fatal("stale signin lost plan", page.Code)
	}
	f.Set("action", "download")
	page = postForm(h, organizePath, f, stale)
	if page.Code != 200 || !json.Valid(page.Body.Bytes()) || page.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private export")
	}
}
