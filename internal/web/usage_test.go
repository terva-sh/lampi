package web

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
)

func publishUsage(t *testing.T, lake *api.Server, uid string, input, output, cached int) {
	t.Helper()
	publishNormalized(t, lake, uid, []normalize.Event{{SchemaVersion: 1, EventID: "u", SessionID: "s", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z",
		Actor: normalize.ActorHarness, EventType: normalize.EventUsage, Usage: normalize.Usage{Input: &input, Output: &output, CacheRead: &cached}, Redaction: normalize.Redaction{Status: "none"}}})
}

// The overview shows tokens split into input, output, cache read and
// cache write, by harness, and counts only the sessions the viewer
// reads (TKT-01M61NTE24T6CZN1P92AV7164F).
func TestOverviewShowsTokensTheViewerReads(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	ctx := t.Context()
	open, _ := storeSession(t, lake, "native-open", []byte("{}\n"))
	secret, _ := storeSession(t, lake, "native-secret", []byte("{}\n{}\n"))
	if _, err := lake.Catalog.CreateBay(ctx, "secret", "test", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.AddToBay(ctx, catalog.Membership{SessionUID: secret, Bay: "secret", Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.RemoveFromBay(ctx, catalog.Membership{SessionUID: secret, Bay: catalog.DefaultBayName, Actor: "test", Via: catalog.ViaCLI}, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Codex counts cached input inside input: 1,500 in with 500 cached
	// is 1,000 plain input.
	publishUsage(t, lake, open, 1500, 250, 500)
	publishUsage(t, lake, secret, 9_000_000, 9_000_000, 0)
	if err := indexes[lake].Pass(ctx); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{`id="usage-heading">Tokens`, `title="1,750">1.75K`, `title="1,000">1.00K`, `title="500">500`, `title="not reported">—`} {
		if !strings.Contains(body, want) {
			t.Fatalf("overview lacks %q", want)
		}
	}
	if strings.Contains(body, "9,000") || strings.Contains(body, "9.00M") {
		t.Fatal("overview counts tokens from a bay the viewer does not read")
	}
	if strings.Contains(body, "are indexed so far") {
		t.Fatal("overview reports a lag with every readable session indexed")
	}
	// A new generation the index has not reached: the viewer, who reads
	// only some bays, is told its totals lag, counting its own sessions.
	publishUsage(t, lake, open, 3000, 500, 1000)
	if body := get(h, "/", cookie).Body.String(); !strings.Contains(body, "0 of 1 ready sessions are indexed so far") {
		t.Fatal("a restricted viewer is not told its totals lag")
	}
}

func TestShortCount(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 987: "987", 1000: "1.00K", 12_400: "12.4K", 3_210_000: "3.21M", 105_000_000: "105M", 1_050_000_000: "1.05B", -2500: "−2.50K"} {
		if got := shortCount(n); got != want {
			t.Errorf("shortCount(%d) = %q, want %q", n, got, want)
		}
	}
}

// The Usage page charts each kind of token, counts only the viewer's
// sessions, and refuses parameters it does not know.
func TestUsagePageChartsTokensOverTime(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid, _ := storeSession(t, lake, "native-usage", []byte("{}\n"))
	input, output, cached := 4200, 300, 200
	now := time.Now().UTC()
	publishNormalized(t, lake, uid, []normalize.Event{{SchemaVersion: 1, EventID: "u", SessionID: "s", Harness: "codex", RecordedAt: now.Add(-2 * time.Hour).Format(time.RFC3339), IngestedAt: now.Format(time.RFC3339Nano),
		Actor: normalize.ActorHarness, EventType: normalize.EventUsage, Usage: normalize.Usage{Input: &input, Output: &output, CacheRead: &cached}, Redaction: normalize.Redaction{Status: "none"}}})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/usage?range=24h&harness=codex", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"<h2>Input tokens</h2>", "<h2>Cache write tokens</h2>", `title="4,000">4.00K`, "1 model calls in this range for codex", `<option value="codex" selected>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("usage page lacks %q", want)
		}
	}
	for _, path := range []string{"/usage?range=1y", "/usage?harness=Codex", "/usage?x=1", "/usage?range=24h&range=7d"} {
		if w := get(h, path, cookie); w.Code != 400 {
			t.Fatalf("%s answered %d", path, w.Code)
		}
	}
	if w := get(h, "/usage?harness=grokbot", cookie); w.Code != 200 || !strings.Contains(w.Body.String(), "0 model calls") {
		t.Fatalf("a harness with no sessions: %d", w.Code)
	}
}

// The Errors tab charts errors and failed results, names the failing
// tool, and links the sessions with the most.
func TestUsageErrorsTab(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid, _ := storeSession(t, lake, "native-errors", []byte("{}\n"))
	now := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	yes := true
	name, id := "shell", "call-1"
	base := normalize.Event{SchemaVersion: 1, SessionID: "s", Harness: "codex", RecordedAt: now, IngestedAt: time.Now().UTC().Format(time.RFC3339Nano), Redaction: normalize.Redaction{Status: "none"}}
	call, result, failure := base, base, base
	call.EventID, call.EventType, call.Actor, call.Tool = "c", normalize.EventToolCall, normalize.ActorAssistant, normalize.Tool{Name: &name, CallID: &id}
	result.EventID, result.EventType, result.Actor, result.Tool = "r", normalize.EventToolResult, normalize.ActorTool, normalize.Tool{CallID: &id, IsError: &yes}
	failure.EventID, failure.EventType, failure.RawType, failure.Actor = "e", normalize.EventError, "stream_error", normalize.ActorHarness
	publishNormalized(t, lake, uid, []normalize.Event{call, result, failure})
	if err := indexes[lake].Pass(t.Context()); err != nil {
		t.Fatal(err)
	}
	cookie, _ := signIn(t, idp, h)
	w := get(h, "/usage/errors?range=24h", cookie)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"<h2>Error events</h2>", "<h2>Failed tool results</h2>", "native-errors", `<span class="mono">shell</span>`, `<span class="mono">stream_error</span>`, "100.0%", `href="/usage/errors?range=24h" aria-current="page"`, `action="/usage/errors"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("errors tab lacks %q", want)
		}
	}
	if w := get(h, "/usage/errors?q=1", cookie); w.Code != 400 {
		t.Fatalf("an unknown parameter answered %d", w.Code)
	}
}
