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
	ctx := context.Background()
	gen, err := lake.Catalog.EnqueueNormalize(ctx, uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	evs := make([]normalize.Event, n)
	for i := range evs {
		txt := text(i)
		evs[i] = normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint(i), SessionID: "s", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z", Actor: normalize.ActorAssistant, EventType: normalize.EventMessage, ContentText: &txt, Redaction: normalize.Redaction{Status: "none"}}
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
	if w.Code != 200 || !strings.Contains(body, `id="e-0"`) || !strings.Contains(body, "Later events") || strings.Contains(body, "Earlier events") {
		t.Fatal("first page", w.Code)
	}
	if strings.Contains(body, "<script>window.owned") || !strings.Contains(body, "&lt;script&gt;window.owned=1&lt;/script&gt;") || strings.Contains(body, "<img") {
		t.Fatal("transcript text not escaped")
	}
	w = get(h, fmt.Sprintf("%s?gen=%d&at=120", page, gen), cookie)
	body = w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `id="e-120" class="event actor-assistant target"`) || !strings.Contains(body, `id="e-115"`) || strings.Contains(body, `id="e-114"`) || !strings.Contains(body, "Earlier events") {
		t.Fatal("deep link", w.Code)
	}
	publishEvents(t, lake, uid, 3, func(int) string { return "rewritten" })
	w = get(h, fmt.Sprintf("%s?gen=%d&at=120", page, gen), cookie)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "This transcript has changed") || !strings.Contains(w.Body.String(), fmt.Sprintf("generation %d", gen)) || strings.Contains(w.Body.String(), "rewritten") {
		t.Fatal("stale link", w.Code)
	}
	if err := os.Remove(filepath.Join(lake.Normalized, uid+".jsonl")); err != nil {
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
