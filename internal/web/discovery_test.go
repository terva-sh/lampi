package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
)

func TestDiscoveryHTTPAndMCPShareScopedMetadata(t *testing.T) {
	lake, _, h, _, _ := mcpLake(t)
	ctx, now := t.Context(), time.Now()
	mine, other := seedSession(t, lake.Catalog, "reviewer"), seedSession(t, lake.Catalog, "development")
	work, err := lake.Catalog.CreateBay(ctx, "reviews", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	private, err := lake.Catalog.CreateBay(ctx, "private-client", "admin", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []catalog.Membership{{SessionUID: mine, Bay: work.ID}, {SessionUID: mine, Bay: private.ID}, {SessionUID: other, Bay: private.ID}} {
		m.Actor, m.Via = "admin", catalog.ViaCLI
		if _, err := lake.Catalog.AddToBay(ctx, m, now); err != nil {
			t.Fatal(err)
		}
	}
	secret := ReadTokenPrefix + "synthetic-discovery"
	if _, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "discovery", Permissions: []string{catalog.PermEventsRead}, BayScoped: true, Bays: []string{work.ID}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, hashReadToken(secret), now); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		tool, path string
		args       map[string]any
	}{
		{"list_sessions", "/api/read/v1/sessions", nil},
		{"list_projects", "/api/read/v1/projects", nil},
		{"list_bays", "/api/read/v1/bays", nil},
		{"get_session", "/api/read/v1/sessions/" + mine, map[string]any{"session_uid": mine}},
	} {
		w := bearer(h, "GET", entry.path, secret)
		text, isError := callTool(t, h, secret, entry.tool, entry.args)
		if w.Code != http.StatusOK || isError {
			t.Fatalf("%s HTTP=%d MCP=%s", entry.tool, w.Code, text)
		}
		var httpBody, mcpBody map[string]any
		if json.Unmarshal(w.Body.Bytes(), &httpBody) != nil || json.Unmarshal([]byte(text), &mcpBody) != nil {
			t.Fatal("invalid JSON")
		}
		delete(httpBody, "as_of")
		delete(mcpBody, "as_of")
		a, _ := json.Marshal(httpBody)
		b, _ := json.Marshal(mcpBody)
		if string(a) != string(b) {
			t.Fatalf("%s parity: %s vs %s", entry.tool, a, b)
		}
		if strings.Contains(text, "private-client") || strings.Contains(text, other) || strings.Contains(text, `"artifacts"`) {
			t.Fatalf("hidden metadata in %s: %s", entry.tool, text)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("metadata may be cached")
		}
	}
	for _, uid := range []string{other, "unknown"} {
		w := bearer(h, "GET", "/api/read/v1/sessions/"+uid, secret)
		text, isError := callTool(t, h, secret, "get_session", map[string]any{"session_uid": uid})
		if w.Code != 404 || !isError || !strings.Contains(text, `"not_found"`) {
			t.Fatalf("inaccessible/unknown: %d %s", w.Code, text)
		}
	}
	for _, query := range []string{"limit=0", "limit=201", "limit=2&limit=3", "cwd_prefix=relative", "unlinked=1", "unknown=value", "cursor="} {
		w := bearer(h, "GET", "/api/read/v1/sessions?"+query, secret)
		if w.Code != 400 {
			t.Errorf("bad query %s: %d", query, w.Code)
		}
	}
	for _, token := range []string{"", ReadTokenPrefix + "unknown"} {
		if w := bearer(h, "GET", "/api/read/v1/sessions", token); w.Code != 401 {
			t.Fatalf("anonymous/unknown status %d", w.Code)
		}
	}
	// A raw-only token must not turn metadata into another raw-read surface.
	raw := ReadTokenPrefix + "synthetic-discovery-raw"
	if _, err := lake.Catalog.CreateReadToken(ctx, catalog.ReadToken{Label: "raw", Permissions: []string{catalog.PermRawRead}, CreatedBy: "admin", Expires: now.Add(time.Hour)}, hashReadToken(raw), now); err != nil {
		t.Fatal(err)
	}
	if w := bearer(h, "GET", "/api/read/v1/sessions", raw); w.Code != 404 {
		t.Fatal(fmt.Sprintf("raw permission exposed discovery: %d", w.Code))
	}
}
