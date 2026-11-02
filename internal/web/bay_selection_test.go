package web

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

func TestBaySelectionAcrossRecallStatisticsDiagnosisAndMetadata(t *testing.T) {
	lake, idp, h, _, index := mcpLake(t)
	codex, claude := normalizedSessions(t, lake, index)
	ctx, now := t.Context(), time.Now()
	a, err := lake.Catalog.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := lake.Catalog.CreateBay(ctx, "development", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []catalog.Membership{{SessionUID: claude, Bay: a.ID}, {SessionUID: claude, Bay: b.ID}, {SessionUID: codex, Bay: b.ID}} {
		m.Actor, m.Via = "admin", catalog.ViaCLI
		if _, err := lake.Catalog.AddToBay(ctx, m, now); err != nil {
			t.Fatal(err)
		}
	}
	admin := signInAs(t, idp, h, "owners")
	token, _ := mintReadTokenWith(t, h, admin, "selection", "", catalog.PermEventsRead)
	for refs, want := range map[string]int64{"reviews": 1, "development": 2, "reviews,development": 2, "reviews,reviews": 1} {
		stats := decodeStats(t, bearer(h, "GET", readToolsPath+"?all=true&bays="+refs, token))
		if stats.Totals.Sessions != want || stats.Coverage != (recall.Coverage{}) {
			t.Fatalf("%s totals %+v", refs, stats)
		}
	}
	for _, entry := range []struct {
		tool string
		args map[string]any
	}{
		{"tool_stats", map[string]any{"all": true, "bays": "reviews"}},
		{"search", map[string]any{"event_type": "tool_call", "bays": "reviews"}},
		{"list_sessions", map[string]any{"bays": "reviews"}},
		{"diagnose_lake", map[string]any{"since": "2020-01-01", "bays": "reviews"}},
	} {
		text, bad := callTool(t, h, token, entry.tool, entry.args)
		if bad || !strings.Contains(text, claude) && entry.tool != "tool_stats" && entry.tool != "diagnose_lake" || strings.Contains(text, codex) {
			t.Fatalf("%s: %s", entry.tool, text)
		}
	}
	for _, tool := range []string{"get_session", "read_events", "copy_excerpt", "diagnose_session"} {
		text, bad := callTool(t, h, token, tool, map[string]any{"session_uid": codex, "bays": "reviews"})
		if !bad || !strings.Contains(text, `"not_found"`) {
			t.Fatalf("single context %s: %s", tool, text)
		}
	}
	// Both search and event continuations are bound to the selected context.
	text, bad := callTool(t, h, token, "read_events", map[string]any{"session_uid": claude, "bays": "reviews", "limit": 1})
	var events recall.EventPage
	if bad || json.Unmarshal([]byte(text), &events) != nil || events.NextCursor == "" {
		t.Fatalf("events %s", text)
	}
	if text, bad := callTool(t, h, token, "read_events", map[string]any{"session_uid": claude, "bays": "development", "cursor": events.NextCursor}); !bad || !strings.Contains(text, `"invalid_request"`) {
		t.Fatalf("cross-selector event cursor: %s", text)
	}
	text, bad = callTool(t, h, token, "search", map[string]any{"event_type": "tool_call", "bays": "reviews", "limit": 1})
	var hits recall.SearchPage
	if bad || json.Unmarshal([]byte(text), &hits) != nil || hits.NextCursor == "" {
		t.Fatalf("search %s", text)
	}
	if text, bad := callTool(t, h, token, "search", map[string]any{"event_type": "tool_call", "bays": "development", "limit": 1, "cursor": hits.NextCursor}); !bad || !strings.Contains(text, `"invalid_request"`) {
		t.Fatalf("cross-selector search cursor: %s", text)
	}
	stream := bearer(h, "GET", readEventsPath+"?event_type=tool_call&bays=reviews", token)
	if stream.Code != 200 || !strings.Contains(stream.Body.String(), "claude:sid-claude") || strings.Contains(stream.Body.String(), "codex:sid-codex") || !strings.Contains(stream.Body.String(), `"complete":true`) {
		t.Fatalf("stream %d %s", stream.Code, stream.Body.String())
	}
	// Unknown and inaccessible selectors return identical bodies.
	secret := ReadTokenPrefix + "synthetic-selection"
	if _, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "limited", Permissions: []string{catalog.PermEventsRead}, BayScoped: true, Bays: []string{a.ID}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, hashReadToken(secret), now); err != nil {
		t.Fatal(err)
	}
	var failureBody string
	for _, ref := range []string{"development", "unknown"} {
		w := bearer(h, "GET", readToolsPath+"?bays="+ref, secret)
		if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_bay_selection") {
			t.Fatalf("selector refusal %d %s", w.Code, w.Body.String())
		}
		if failureBody != "" && failureBody != w.Body.String() {
			t.Fatal("selector existence leaked")
		}
		failureBody = w.Body.String()
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: claude, Bay: a.ID, Actor: "admin", Via: catalog.ViaCLI}, now); err != nil {
		t.Fatal(err)
	}
	w := bearer(h, "GET", "/api/read/v1/sessions/"+claude+"?bays=reviews", token)
	if w.Code != 404 {
		t.Fatalf("membership removal still readable %d %s", w.Code, w.Body.String())
	}
	w = bearer(h, "GET", "/api/read/v1/sessions/"+claude+"/events?bays=reviews&cursor="+url.QueryEscape(events.NextCursor), token)
	if w.Code != 404 {
		t.Fatalf("cursor bypassed current membership: %d", w.Code)
	}
}
