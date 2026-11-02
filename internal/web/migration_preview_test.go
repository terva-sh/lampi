package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

func postPreview(h http.Handler, path, token string, args any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(args)
	r := httptest.NewRequest("POST", "https://lake.example"+path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUnrestrictedReadMigrationPreviewDoesNotExposeGrantPrincipals(t *testing.T) {
	lake, idp, h, _, _ := mcpLake(t)
	ctx, now := t.Context(), time.Now()
	uid := seedSession(t, lake.Catalog, "reviewer")
	dst, err := lake.Catalog.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range []struct{ bay, principal string }{
		{"default", "source-reader-sentinel"}, {dst.ID, "destination-reader-sentinel"},
	} {
		if _, err := lake.Catalog.AddGrant(ctx, catalog.PrincipalGroup, grant.principal, grant.bay, catalog.PermRead, "admin", now); err != nil {
			t.Fatal(err)
		}
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "unrestricted-previews", "", catalog.PermEventsRead)
	args := map[string]any{"session_uids": []string{uid}, "add": []string{dst.ID}, "remove": []string{"default"}}
	mcpText, bad := callTool(t, h, token, "preview_bay_migration", args)
	if bad {
		t.Fatalf("MCP preview: %s", mcpText)
	}
	w := postPreview(h, "/api/read/v1/preview-bay-migration", token, args)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP preview: %d %s", w.Code, w.Body.String())
	}
	for _, raw := range []string{mcpText, w.Body.String()} {
		var plan catalog.BayMigrationPlan
		if err := json.Unmarshal([]byte(raw), &plan); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(raw, "reader-sentinel") || plan.CompleteMemberships || plan.Changed != 1 || plan.Rejected != 0 {
			t.Fatalf("unrestricted reader preview: %s", raw)
		}
		if len(plan.Destinations) != 1 || !strings.Contains(plan.Destinations[0].Access, "Existing destination readers") {
			t.Fatalf("missing generic readership warning: %s", raw)
		}
		if _, err := lake.Catalog.ApplyBayMigration(ctx, catalog.AllBays(), true, plan, false, "admin", catalog.ViaCLI, now); !errors.Is(err, catalog.ErrMigrationAuthority) {
			t.Fatalf("read-token plan bypassed administrator re-preview: %v", err)
		}
	}
}

func TestMigrationPreviewMCPHTTPParityScopeAndNoMutation(t *testing.T) {
	lake, idp, h, dir, _ := mcpLake(t)
	ctx, now := t.Context(), time.Now()
	uid, companion := seedSession(t, lake.Catalog, "reviewer"), seedSession(t, lake.Catalog, "rater")
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "previews", "", catalog.PermEventsRead)
	for _, entry := range []struct {
		name string
		args map[string]any
	}{
		{"select_sessions", map[string]any{"session_uids": []string{uid, companion, uid}, "exclude": []string{companion}}},
		{"select_sessions", map[string]any{"filter": map[string]any{"harness": "codex"}, "exclude": []string{companion}}},
		{"preview_bay_migration", map[string]any{"session_uids": []string{uid, companion}, "add": []string{"code-reviews", "learning"}, "remove": []string{"default"}, "proposed_bays": []string{"code-reviews", "learning"}, "reason": "private-preview-reason-sentinel"}},
		{"preview_bay_rule", map[string]any{"match": map[string]any{"cwd_prefix": "/review-layout"}, "action": "add", "bay": "code-reviews", "proposed_bay": true}},
	} {
		text, bad := callTool(t, h, token, entry.name, entry.args)
		w := postPreview(h, "/api/read/v1/"+strings.ReplaceAll(entry.name, "_", "-"), token, entry.args)
		if bad || w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: MCP %s HTTP %d %s", entry.name, text, w.Code, w.Body.String())
		}
		var a, b map[string]any
		if json.Unmarshal([]byte(text), &a) != nil || json.Unmarshal(w.Body.Bytes(), &b) != nil {
			t.Fatal("invalid JSON")
		}
		for _, key := range []string{"plan_id", "created_at", "expires_at", "as_of"} {
			delete(a, key)
			delete(b, key)
		}
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if string(aa) != string(bb) {
			t.Fatalf("%s parity %s vs %s", entry.name, aa, bb)
		}
		if entry.name == "preview_bay_migration" {
			var plan catalog.BayMigrationPlan
			if err := json.Unmarshal([]byte(text), &plan); err != nil {
				t.Fatal(err)
			}
			if len(plan.Items) != 2 || plan.Changed != 2 || plan.Rejected != 0 {
				t.Fatalf("plan %+v", plan)
			}
			if _, err := lake.Catalog.ApplyBayMigration(ctx, catalog.AllBays(), false, plan, true, "reader", catalog.ViaCLI, now); !errors.Is(err, catalog.ErrMigrationAuthority) {
				t.Fatal("plan minted authority")
			}
			if _, err := lake.Catalog.VerifyBayMigration(ctx, catalog.AllBays(), true, plan.ID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("preview persisted a receipt %v", err)
			}
		}
	}
	for _, uid := range []string{uid, companion} {
		bays, _ := lake.Catalog.SessionBays(ctx, uid)
		if len(bays) != 1 || bays[0] != catalog.DefaultBayID {
			t.Fatal("preview changed membership")
		}
	}
	if _, err := lake.Catalog.ResolveBay(ctx, "code-reviews"); !errors.Is(err, catalog.ErrNoBay) {
		t.Fatal("preview created target")
	}
	rules, _ := lake.Catalog.BayRules(ctx)
	if len(rules) != 0 {
		t.Fatal("preview installed rule")
	}
	log, err := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if err != nil || strings.Contains(string(log), "private-preview-reason-sentinel") || !strings.Contains(string(log), "mcp tool=preview_bay_migration") || !strings.Contains(string(log), "session_uids=2+items") {
		t.Fatalf("bounded audit %v", err)
	}
}

func TestMigrationPreviewRestrictedHandoffStalenessAndInvalidInput(t *testing.T) {
	lake, _, h, _, _ := mcpLake(t)
	ctx, now := t.Context(), time.Now()
	uid, hiddenUID := seedSession(t, lake.Catalog, "reviewer"), seedSession(t, lake.Catalog, "hidden")
	visible, _ := lake.Catalog.CreateBay(ctx, "visible", "admin", now)
	hidden, _ := lake.Catalog.CreateBay(ctx, "private-client", "admin", now)
	for _, m := range []catalog.Membership{{SessionUID: uid, Bay: visible.ID}, {SessionUID: uid, Bay: hidden.ID}, {SessionUID: hiddenUID, Bay: hidden.ID}} {
		m.Actor, m.Via = "admin", catalog.ViaCLI
		if _, err := lake.Catalog.AddToBay(ctx, m, now); err != nil {
			t.Fatal(err)
		}
	}
	token := ReadTokenPrefix + "synthetic-preview-scoped"
	if _, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "scope", Permissions: []string{catalog.PermEventsRead}, BayScoped: true, Bays: []string{visible.ID}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, hashReadToken(token), now); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"session_uids": []string{uid, hiddenUID}, "add": []string{"proposed"}, "proposed_bays": []string{"proposed"}}
	text, bad := callTool(t, h, token, "preview_bay_migration", args)
	var plan catalog.BayMigrationPlan
	if bad || json.Unmarshal([]byte(text), &plan) != nil || plan.Rejected != 1 || plan.CompleteMemberships || strings.Contains(text, "private-client") || strings.Contains(text, hidden.ID) || strings.Contains(text, "bay_default") {
		t.Fatalf("scope %s", text)
	}
	if _, err := lake.Catalog.ApplyBayMigration(ctx, catalog.AllBays(), true, plan, true, "admin", catalog.ViaCLI, now); !errors.Is(err, catalog.ErrMigrationAuthority) {
		t.Fatalf("scoped handoff bypassed full review: %v", err)
	}
	// Operator extracts exact intent, excludes rejected IDs, and re-previews
	// with full authority. A later material change still blocks apply.
	request := plan.Request
	request.Exclude = []string{hiddenUID}
	full, err := lake.Catalog.PlanBayMigration(ctx, catalog.AllBays(), true, request, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: uid, Bay: "default", Actor: "later", Via: catalog.ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	// Existing default is a no-op, so change the destination policy instead.
	if err := lake.Catalog.RenameBay(ctx, visible.ID, "renamed", "admin", now); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.ApplyBayMigration(ctx, catalog.AllBays(), true, full, true, "admin", catalog.ViaCLI, now); !errors.Is(err, catalog.ErrMigrationStale) {
		t.Fatalf("stale supplied plan %v", err)
	}
	for _, entry := range []struct {
		name string
		args any
	}{
		{"select-sessions", map[string]any{"filter": map[string]any{"cwd_prefix": "relative"}}},
		{"select-sessions", map[string]any{"session_uids": []string{uid}, "filter": map[string]any{"harness": "claude"}}},
		{"select-sessions", map[string]any{"session_uids": make([]string, 501)}},
		{"select-sessions", map[string]any{"filter": map[string]any{"harness": "claude", "limit": 0}}},
		{"preview-bay-migration", map[string]any{"session_uids": []string{uid}, "add": []string{visible.ID}, "edits": []any{}}},
		{"preview-bay-rule", map[string]any{"harness": "claude", "bay": "x", "proposed_bay": true, "action": "add"}},
		{"preview-bay-migration", map[string]any{"session_uids": []string{uid}, "add": []string{hidden.ID}}},
		{"preview-bay-migration", map[string]any{"session_uids": []string{uid}, "add": []string{visible.ID}, "write": true}},
		{"preview-bay-rule", map[string]any{"match": map[string]any{"cwd_glob": "["}, "bay": "x", "proposed_bay": true, "action": "add"}},
	} {
		w := postPreview(h, "/api/read/v1/"+entry.name, token, entry.args)
		if w.Code != 400 {
			t.Fatalf("invalid %s: %d %s", entry.name, w.Code, w.Body.String())
		}
	}
	if w := postPreview(h, "/api/read/v1/select-sessions", "", map[string]any{"session_uids": []string{uid}}); w.Code != 401 {
		t.Fatalf("anonymous %d", w.Code)
	}
	for _, name := range []string{"apply_bay_migration", "create_bay", "install_bay_rule"} {
		reply := decodeRPC(t, postRPC(h, token, rpcBody(3, "tools/call", map[string]any{"name": name, "arguments": map[string]any{}}), nil))
		if reply.Error == nil && reply.Result["isError"] != true {
			t.Fatalf("mutation tool exists: %s", name)
		}
	}
}
