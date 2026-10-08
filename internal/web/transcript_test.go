package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/normalize"
)

func publishEvents(t *testing.T, lake *api.Server, uid string, n int, text func(int) string) int64 {
	t.Helper()
	evs := make([]normalize.Event, n)
	for i := range evs {
		txt := text(i)
		evs[i] = normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint(i), SessionID: "s", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z", Actor: normalize.ActorAssistant, EventType: normalize.EventMessage, ContentText: &txt, Redaction: normalize.Redaction{Status: "none"}}
	}
	return publishNormalized(t, lake, uid, evs)
}

// publishNormalized publishes evs as uid's transcript and returns the
// generation.
func publishNormalized(t *testing.T, lake *api.Server, uid string, evs []normalize.Event) int64 {
	t.Helper()
	ctx := context.Background()
	gen, err := lake.Catalog.EnqueueNormalize(ctx, uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := lake.StoreEvents(ctx, uid, evs, nil); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.DeleteNormalizeJob(ctx, uid, gen); err != nil {
		t.Fatal(err)
	}
	return gen
}

func TestEventsAPIIsGuardedBoundedAndPinned(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "events")
	cookie, _ := signIn(t, idp, h)
	path := "/api/web/v1/sessions/" + uid + "/events"
	if get(h, path, nil).Code != 401 {
		t.Fatal("unguarded events")
	}
	w := get(h, path, cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"state":"unknown"`) || !strings.Contains(w.Body.String(), "transcript_unavailable") {
		t.Fatal("unpublished", w.Code, w.Body.String())
	}
	gen := publishEvents(t, lake, uid, 30, func(i int) string { return fmt.Sprintf("<b>event %d</b>", i) })
	w = get(h, path+"?limit=10", cookie)
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("events", w.Code, w.Body.String())
	}
	var page struct {
		Generation int64  `json:"generation"`
		NextCursor string `json:"next_cursor"`
		Items      []struct {
			Position int64 `json:"position"`
			Event    struct {
				ContentText string `json:"content_text"`
			} `json:"event"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Generation != gen || len(page.Items) != 10 || page.Items[3].Event.ContentText != "<b>event 3</b>" {
		t.Fatal("page", err, w.Body.String())
	}
	w = get(h, path+"?cursor="+url.QueryEscape(page.NextCursor), cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"position":10`) {
		t.Fatal("cursor page", w.Code)
	}
	for _, bad := range []string{"?limit=0", "?limit=201", "?from=-1", "?from=x", "?gen=-1", "?cursor=", "?cursor=zz", "?x=1", "?from=1&from=2"} {
		if w := get(h, path+bad, cookie); w.Code != 400 {
			t.Fatal("accepted", bad, w.Code)
		}
	}
	if w := get(h, path+fmt.Sprintf("?gen=%d", gen+1), cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "generation_changed") {
		t.Fatal("pinned", w.Code, w.Body.String())
	}
	if w := get(h, "/api/web/v1/sessions/missing/events", cookie); w.Code != 404 {
		t.Fatal("missing", w.Code)
	}
	if w := get(h, "/api/web/v1/sessions/..%2f..%2fetc/events", cookie); w.Code == 200 {
		t.Fatal("path escape", w.Code)
	}
	publishEvents(t, lake, uid, 5, func(int) string { return "newer" })
	if w := get(h, path+"?cursor="+url.QueryEscape(page.NextCursor), cookie); w.Code != 409 {
		t.Fatal("stale cursor", w.Code)
	}
}

func TestTranscriptPageRendersLiterallyAndHandlesStaleLinks(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "page")
	cookie, _ := signIn(t, idp, h)
	page := "/sessions/" + uid + "/transcript"
	if w := get(h, page, nil); w.Code != 303 {
		t.Fatal("unguarded page", w.Code)
	}
	if w := get(h, page, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "No published transcript") || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("unpublished page", w.Code)
	}
	gen := publishEvents(t, lake, uid, 150, func(i int) string {
		if i == 42 {
			return `<script>window.owned=1</script><img src=x onerror=alert(1)>`
		}
		return fmt.Sprint("event ", i)
	})
	w := get(h, page, cookie)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `id="e-0"`) || !strings.Contains(body, `aria-label="Next page"`) || strings.Contains(body, `aria-label="Previous page"`) {
		t.Fatal("first page", w.Code)
	}
	if strings.Contains(body, "<script>window.owned") || !strings.Contains(body, "&lt;script&gt;window.owned=1&lt;/script&gt;") || strings.Contains(body, "<img") {
		t.Fatal("transcript text not escaped")
	}
	w = get(h, fmt.Sprintf("%s?gen=%d&at=120", page, gen), cookie)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `id="e-120" class="event actor-assistant target"`) || !strings.Contains(body, `id="e-115"`) || strings.Contains(body, `id="e-114"`) || !strings.Contains(body, `aria-label="Previous page"`) {
		t.Fatal("deep link", w.Code)
	}
	publishEvents(t, lake, uid, 3, func(int) string { return "rewritten" })
	w = get(h, fmt.Sprintf("%s?gen=%d&at=120", page, gen), cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "This transcript has changed") || !strings.Contains(w.Body.String(), fmt.Sprintf("generation %d", gen)) || strings.Contains(w.Body.String(), "rewritten") {
		t.Fatal("stale link", w.Code)
	}
	if err := os.Remove(filepath.Join(lake.Normalized, uid+normalize.EventsExt)); err != nil {
		t.Fatal(err)
	}
	if w := get(h, page, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "Derived file missing") {
		t.Fatal("missing file", w.Code)
	}
	for _, bad := range []string{"?at=-1", "?at=x", "?at=1&cursor=abc", "?nope=1"} {
		if w := get(h, page+bad, cookie); w.Code != 400 {
			t.Fatal("bad page input accepted", bad, w.Code)
		}
	}
	if w := get(h, "/sessions/"+uid, cookie); !strings.Contains(w.Body.String(), `href="/sessions/`+uid+`/transcript">Read transcript`) {
		t.Fatal("detail page links a ready transcript")
	}
	if _, err := lake.Catalog.EnqueueNormalize(t.Context(), uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	if w := get(h, "/sessions/"+uid, cookie); !strings.Contains(w.Body.String(), "Available once normalization is ready") {
		t.Fatal("detail link should wait for ready")
	}
	if w := get(h, page, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "Normalization is running") {
		t.Fatal("pending page", w.Code)
	}
}

func TestDeepLinksReportEveryUnavailableState(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "links")
	cookie, _ := signIn(t, idp, h)
	gen := publishEvents(t, lake, uid, 10, func(i int) string { return fmt.Sprint("event ", i) })
	link := fmt.Sprintf("/sessions/%s/transcript?gen=%d&at=", uid, gen)
	w := get(h, link+"9999", cookie)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Event 9999 is past the end of this transcript, which has 10 events.") {
		t.Fatal("out of range", w.Code, w.Body.String())
	}
	if w := get(h, link+"3", cookie); !strings.Contains(w.Body.String(), `id="e-3" class="event actor-assistant target"`) {
		t.Fatal("in range")
	}
	if _, err := lake.Catalog.EnqueueNormalize(t.Context(), uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	w = get(h, link+"3", cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "Event positions can change when it lands.") || !strings.Contains(w.Body.String(), `href="/sessions/`+uid+`">Session details`) {
		t.Fatal("pending link", w.Code)
	}
	gen2, _, _, _ := lake.Catalog.NormalizeVersion(t.Context(), uid)
	if err := lake.StoreEvents(t.Context(), uid, nil, fmt.Errorf("synthetic")); err != nil {
		t.Fatal(err)
	}
	if err := lake.Catalog.DeleteNormalizeJob(t.Context(), uid, gen2); err != nil {
		t.Fatal(err)
	}
	if w := get(h, link+"3", cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "Normalization failed") {
		t.Fatal("failed link", w.Code)
	}
	plan, _, _ := lake.PlanPurge(t.Context(), uid)
	if err := lake.Purge(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	w = get(h, link+"3", cookie)
	if w.Code != 404 || !strings.Contains(w.Body.String(), "This session is not in the lake") || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("purged link", w.Code, w.Body.String())
	}
}

func TestExcerptAPIAndPlainPage(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "copy")
	cookie, _ := signIn(t, idp, h)
	gen := publishEvents(t, lake, uid, 20, func(i int) string {
		if i == 5 {
			return `<script>window.owned=1</script>`
		}
		return fmt.Sprint("event ", i)
	})
	api := fmt.Sprintf("/api/web/v1/sessions/%s/excerpt?gen=%d&from=4&count=3", uid, gen)
	plain := fmt.Sprintf("/sessions/%s/excerpt?gen=%d&from=4&count=3", uid, gen)
	if get(h, api, nil).Code != 401 || get(h, plain, nil).Code != 303 {
		t.Fatal("unguarded excerpt")
	}
	w := get(h, api, cookie)
	var ex struct {
		Text   string `json:"text"`
		Events int    `json:"events"`
		Link   string `json:"link"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ex) != nil || ex.Events != 3 || !strings.HasPrefix(ex.Link, "https://lake.example/sessions/") {
		t.Fatal("api", w.Code, w.Body.String())
	}
	w = get(h, plain, cookie)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Body.String() != ex.Text {
		t.Fatal("plain page", w.Code, w.Header())
	}
	if !strings.Contains(w.Body.String(), "<script>window.owned=1</script>") {
		t.Fatal("plain text should hold the literal text")
	}
	for _, bad := range []string{"?count=0", "?count=201", "?from=-1", "?from=500", "?x=1", "?gen=-1"} {
		if w := get(h, "/api/web/v1/sessions/"+uid+"/excerpt"+bad, cookie); w.Code != 400 {
			t.Fatal("api accepted", bad, w.Code)
		}
		if w := get(h, "/sessions/"+uid+"/excerpt"+bad, cookie); w.Code != 400 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
			t.Fatal("page accepted", bad, w.Code)
		}
	}
	publishEvents(t, lake, uid, 2, func(int) string { return "newer" })
	if w := get(h, plain, cookie); w.Code != 409 || !strings.Contains(w.Body.String(), "has changed") {
		t.Fatal("stale plain", w.Code)
	}
	w = get(h, "/sessions/"+uid+"/transcript", cookie)
	if !strings.Contains(w.Body.String(), "This page as plain text") || !strings.Contains(w.Body.String(), `id="excerpt-bar"`) {
		t.Fatal("transcript copy controls")
	}
}

// TestTranscriptFoldsRunsOfQuietUnknownEvents: three or more unknown
// events with no text in a row fold into one <details> naming the
// range and each raw type (TKT-01M3SZQ69B). Shorter stretches, and an
// unknown event that carries text, stay as cards.
func TestTranscriptFoldsRunsOfQuietUnknownEvents(t *testing.T) {
	lake, idp, h, _ := fixture(t)
	uid := seedSession(t, lake.Catalog, "runs")
	cookie, _ := signIn(t, idp, h)
	ev := func(i int, eventType, actor, raw, text string, extra map[string]any) normalize.Event {
		e := normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint(i), SessionID: "s", Harness: "claude", RecordedAt: "2026-09-28T06:49:52Z", IngestedAt: "2026-09-28T06:49:53Z", Actor: actor, EventType: eventType, RawType: raw, Extra: extra, Redaction: normalize.Redaction{Status: "none"}}
		if text != "" {
			e.ContentText = &text
		}
		return e
	}
	msg := func(i int) normalize.Event {
		return ev(i, normalize.EventMessage, normalize.ActorUser, "user", fmt.Sprint("message ", i), nil)
	}
	quiet := func(i int, raw string, extra map[string]any) normalize.Event {
		return ev(i, normalize.EventUnknown, normalize.ActorHarness, raw, "", extra)
	}
	local := map[string]any{"subtype": "local_command"}
	evs := []normalize.Event{
		msg(0),
		quiet(1, "file-history-snapshot", nil), quiet(2, "queue-operation", nil), quiet(3, "file-history-snapshot", nil),
		msg(4),
		quiet(5, "file-history-snapshot", nil), quiet(6, "file-history-snapshot", nil),
		msg(7),
		quiet(8, "file-history-snapshot", nil), quiet(9, "file-history-snapshot", nil),
		ev(10, normalize.EventUnknown, normalize.ActorHarness, "ai-title", "Talkoot avatar notes", nil),
		quiet(11, "system", local), quiet(12, "system", local), quiet(13, "system", local),
		msg(14),
	}
	publishNormalized(t, lake, uid, evs)
	page := "/sessions/" + uid + "/transcript"
	w := get(h, page, cookie)
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatal("page", w.Code)
	}
	for _, want := range []string{
		`<details id="run-1">`,
		`3 unknown events</span><span class="mono">#1–#3</span><span class="sub-inline">file-history-snapshot ×2, queue-operation ×1</span>`,
		`id="e-2" class="event actor-harness"`,
		`<details id="run-11">`,
		`system/local_command ×3`,
		`<span class="etype">unknown</span><span class="harness">file-history-snapshot</span>`,
		`<span class="etype">unknown</span><span class="harness">system/local_command</span>`,
		`Talkoot avatar notes`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Two in a row, and two cut short by a titled unknown event, are
	// not runs. The titled event is never folded.
	for _, bad := range []string{`id="run-5"`, `id="run-8"`, `id="run-9"`, `id="run-10"`, `id="run-0"`} {
		if strings.Contains(body, bad) {
			t.Errorf("page folds %s", bad)
		}
	}
	for i := range 15 {
		if !strings.Contains(body, fmt.Sprintf(`id="e-%d"`, i)) {
			t.Errorf("event %d has no card", i)
		}
	}
	// Every card of the run sits inside the run's <details>.
	start := strings.Index(body, `<details id="run-11">`)
	if start < 0 {
		t.Fatal("no run 11 on the page")
	}
	end := strings.Index(body[start:], `</details>`) + start
	for _, id := range []string{`id="e-11"`, `id="e-12"`, `id="e-13"`} {
		if !strings.Contains(body[start:end], id) {
			t.Errorf("%s is not inside run 11", id)
		}
	}
	// A deep link into a run unfolds it and marks the event.
	body = get(h, page+"?at=12&from=0", cookie).Body.String()
	if !strings.Contains(body, `<details id="run-11" open>`) || !strings.Contains(body, `id="e-12" class="event actor-harness target"`) || !strings.Contains(body, `<details id="run-1">`) {
		t.Fatal("deep link into a run does not unfold it")
	}
}
