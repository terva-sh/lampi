package recall

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func lake(t *testing.T) *api.Server {
	t.Helper()
	s, err := api.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ingest(t *testing.T, s *api.Server, native string) string {
	t.Helper()
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "codex", NativeSessionID: native,
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "s.jsonl", SHA256: strings.Repeat("b", 64), Size: 12}}}
	ack, err := s.Catalog.Ingest(t.Context(), m, time.Now(), []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ack.SessionUID
}

// publish runs one generation through the same path a worker takes.
func publish(t testing.TB, s *api.Server, uid string, events []normalize.Event) int64 {
	t.Helper()
	ctx := context.Background()
	gen, err := s.Catalog.EnqueueNormalize(ctx, uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.StoreEvents(ctx, uid, events, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Catalog.DeleteNormalizeJob(ctx, uid, gen); err != nil {
		t.Fatal(err)
	}
	return gen
}

func events(n int, text func(i int) string) []normalize.Event {
	out := make([]normalize.Event, n)
	for i := range out {
		txt := text(i)
		out[i] = normalize.Event{SchemaVersion: 1, EventID: fmt.Sprint("ev-", i), SessionID: "native", Harness: "codex", RecordedAt: "2026-09-26T10:00:00Z", IngestedAt: "2026-09-26T10:00:01Z", Actor: normalize.ActorUser, EventType: normalize.EventMessage, ContentText: &txt, Redaction: normalize.Redaction{Status: "none"}}
	}
	return out
}

func TestEventsPageThroughCursorAndPositions(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "paging")
	gen := publish(t, s, uid, events(250, func(i int) string { return fmt.Sprint("line ", i) }))
	r := NewReader(s.Catalog, s.Normalized)
	var seen int64
	req := EventRequest{Limit: 100}
	for pages := 0; ; pages++ {
		p, err := r.Events(t.Context(), catalog.AllBays(), uid, req)
		if err != nil {
			t.Fatal(err)
		}
		if p.Generation != gen {
			t.Fatal("generation", p.Generation, gen)
		}
		for _, it := range p.Items {
			if it.Position != seen || *it.Event.ContentText != fmt.Sprint("line ", seen) {
				t.Fatal("order", it.Position, seen)
			}
			if it.Link != EventLink(uid, gen, seen) {
				t.Fatal("link", it.Link)
			}
			seen++
		}
		if p.End {
			if p.NextCursor != "" || pages != 2 {
				t.Fatal("end", pages, p.NextCursor)
			}
			break
		}
		req = EventRequest{Limit: 100, Cursor: p.NextCursor}
	}
	if seen != 250 {
		t.Fatal("seen", seen)
	}
	p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{From: 240, Limit: 5})
	if err != nil || p.From != 240 || p.Items[0].Position != 240 || len(p.Items) != 5 || p.End || *p.PrevFrom != 235 {
		t.Fatal("from", err, p.From)
	}
	p, err = r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{From: 245, Limit: 5})
	if err != nil || !p.End || p.NextCursor != "" {
		t.Fatal("exact end", err, p.End)
	}
	p, err = r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{From: 900})
	if err != nil || len(p.Items) != 0 || !p.End || p.From != 250 {
		t.Fatal("past end", err, p.From)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Gen: gen + 1, Pinned: true}); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal("pinned generation", err)
	}
}

func TestEventsBoundContentAndHideOpaque(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "bounds")
	long := strings.Repeat("é", ContentPreview) // two bytes a rune
	evs := events(60, func(i int) string {
		if i == 0 {
			return `<script>alert(1)</script> & "quoted"`
		}
		return long
	})
	evs[0].Extra = map[string]any{"encrypted_content": "Z2liYmVyaXNo", "nested": map[string]any{"sealed_box": "x", "keep": 1}}
	evs[1].Extra = map[string]any{"blob": strings.Repeat("x", ExtraBytes)}
	publish(t, s, uid, evs)
	r := NewReader(s.Catalog, s.Normalized)
	p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	first := p.Items[0]
	if *first.Event.ContentText != `<script>alert(1)</script> & "quoted"` || !first.Opaque || first.Truncated {
		t.Fatal("literal text or opaque flag", first)
	}
	b, _ := json.Marshal(first)
	if strings.Contains(string(b), "Z2liYmVyaXNo") || strings.Contains(string(b), "sealed_box") || !strings.Contains(string(b), `"keep":1`) {
		t.Fatal("opaque content leaked", string(b))
	}
	second := p.Items[1]
	if !second.Truncated || second.ContentBytes != len(long) || len(*second.Event.ContentText) > ContentPreview || !second.ExtraOmitted {
		t.Fatal("bounds", second.Truncated, second.ContentBytes, second.ExtraOmitted)
	}
	if !strings.HasSuffix(*second.Event.ContentText, "é") {
		t.Fatal("cut inside a rune")
	}
	if len(p.Items) >= 60 || p.NextCursor == "" {
		t.Fatal("page bytes not bounded", len(p.Items))
	}
	enc, _ := json.Marshal(p.Items)
	if len(enc) > PageBytes {
		t.Fatal("page over cap", len(enc))
	}
}

func TestEventsUnavailableStatesAndBadInput(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "states")
	r := NewReader(s.Catalog, s.Normalized)
	var unavailable UnavailableError
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{}); !errors.As(err, &unavailable) || unavailable.State != "unknown" {
		t.Fatal("never published", err)
	}
	publish(t, s, uid, events(3, func(int) string { return "a" }))
	if _, err := s.Catalog.EnqueueNormalize(t.Context(), uid, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{}); !errors.As(err, &unavailable) || unavailable.State != "pending" {
		t.Fatal("pending", err)
	}
	publish(t, s, uid, events(3, func(int) string { return "b" }))
	if err := os.Remove(filepath.Join(s.Normalized, uid+normalize.EventsExt)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{}); !errors.As(err, &unavailable) || unavailable.State != "missing" {
		t.Fatal("missing", err)
	}
	if err := s.StoreEvents(t.Context(), uid, nil, errors.New("synthetic failure")); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{}); !errors.As(err, &unavailable) || unavailable.State != "failed" {
		t.Fatal("failed", err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), "nope", EventRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown uid", err)
	}
	for _, bad := range []string{"", "../x", "a/b", strings.Repeat("u", 129)} {
		if _, err := r.Events(t.Context(), catalog.AllBays(), bad, EventRequest{}); !errors.Is(err, ErrInvalid) {
			t.Fatal("uid accepted", bad, err)
		}
	}
	for _, req := range []EventRequest{{Limit: 201}, {Limit: -1}, {From: -1}, {Cursor: "x"}, {Cursor: "e30.AAAA"}} {
		if _, err := r.Events(t.Context(), catalog.AllBays(), uid, req); !errors.Is(err, ErrInvalid) {
			t.Fatal("request accepted", req, err)
		}
	}
}

func TestCursorRejectsTamperingAndNewGenerations(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "cursor")
	other := ingest(t, s, "other")
	publish(t, s, uid, events(10, func(int) string { return "one" }))
	publish(t, s, other, events(10, func(int) string { return "two" }))
	r := NewReader(s.Catalog, s.Normalized)
	p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), other, EventRequest{Cursor: p.NextCursor}); !errors.Is(err, ErrInvalid) {
		t.Fatal("cursor moved between sessions", err)
	}
	if _, err := NewReader(s.Catalog, s.Normalized).Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: p.NextCursor}); !errors.Is(err, ErrInvalid) {
		t.Fatal("cursor survived a new key", err)
	}
	c, _ := r.verify(p.NextCursor)
	c.Off = 1
	body, _ := json.Marshal(c)
	forged := strings.Replace(p.NextCursor, strings.SplitN(p.NextCursor, ".", 2)[0], encodeRaw(body), 1)
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: forged}); !errors.Is(err, ErrInvalid) {
		t.Fatal("forged offset accepted", err)
	}
	publish(t, s, uid, events(10, func(int) string { return "new" }))
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: p.NextCursor}); !errors.Is(err, ErrGenerationChanged) {
		t.Fatal("stale cursor", err)
	}
}

// TestPagesNeverMixGenerations republishes while reading. Every event
// names its generation, so a page that mixed two would show it. Each
// served page is checked, so a few pages across at least one reload
// exercise it; the floor stays low because a loaded runner serves few
// pages while the writer keeps forcing reloads.
func TestPagesNeverMixGenerations(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "race")
	mark := func(gen int64) []normalize.Event {
		return events(400, func(i int) string { return fmt.Sprintf("gen-%d %s", gen, strings.Repeat("p", i%50)) })
	}
	publish(t, s, uid, mark(1))
	r := NewReader(s.Catalog, s.Normalized)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for next := int64(2); ; next++ {
			select {
			case <-stop:
				return
			default:
			}
			publish(t, s, uid, mark(next))
			time.Sleep(10 * time.Millisecond)
		}
	}()
	const minPages = 10
	var served, reloads int
	deadline := time.Now().Add(30 * time.Second)
	for (served < minPages || reloads == 0) && time.Now().Before(deadline) {
		p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Limit: 200})
		var unavailable UnavailableError
		if errors.As(err, &unavailable) || errors.Is(err, ErrGenerationChanged) {
			reloads++
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprintf("gen-%d ", p.Generation)
		for _, it := range p.Items {
			if !strings.HasPrefix(*it.Event.ContentText, want) {
				t.Fatalf("page of generation %d holds %q", p.Generation, (*it.Event.ContentText)[:8])
			}
		}
		if p.NextCursor != "" {
			if q, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: p.NextCursor}); err == nil {
				for _, it := range q.Items {
					if !strings.HasPrefix(*it.Event.ContentText, want) {
						t.Fatal("cursor page crossed generations")
					}
				}
			}
		}
		served++
	}
	close(stop)
	wg.Wait()
	t.Logf("served %d pages, %d reloads", served, reloads)
	if served < minPages || reloads == 0 {
		t.Fatal("race not exercised", served, reloads)
	}
}

func TestReadLineBoundsLongLines(t *testing.T) {
	in := strings.Repeat("x", 100) + "\n" + "short\n" + "tail"
	br := bufio.NewReaderSize(strings.NewReader(in), 16)
	line, n, err := readLine(br, 50)
	if line != nil || n != 101 || err != nil {
		t.Fatal("long line", line, n, err)
	}
	line, n, err = readLine(br, 50)
	if string(line) != "short" || n != 6 || err != nil {
		t.Fatal("short line", string(line), n, err)
	}
	line, n, err = readLine(br, 50)
	if string(line) != "tail" || n != 4 || err == nil {
		t.Fatal("tail", string(line), n, err)
	}
	if it := decodeItem("u", 1, 7, nil); !it.Oversized || it.Position != 7 || it.Event != nil {
		t.Fatal("placeholder", it)
	}
	if it := decodeItem("u", 1, 7, []byte("{")); !it.Unreadable {
		t.Fatal("unreadable", it)
	}
}

func encodeRaw(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func TestPurgeEndsReadsAndOpenSnapshotsFinish(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "purge")
	publish(t, s, uid, events(20, func(i int) string { return fmt.Sprint("kept ", i) }))
	r := NewReader(s.Catalog, s.Normalized)
	first, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	plan, ok, err := s.PlanPurge(t.Context(), uid)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if err := s.Purge(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{}); !errors.Is(err, ErrNotFound) {
		t.Fatal("purged session still readable", err)
	}
	if _, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{Cursor: first.NextCursor}); !errors.Is(err, ErrNotFound) {
		t.Fatal("cursor outlived purge", err)
	}
}

// pageAll reads every event of uid through cursors and checks each
// position's text, returning how many it saw.
func pageAll(t *testing.T, r *Reader, uid string, text func(i int) string) int64 {
	t.Helper()
	var seen int64
	req := EventRequest{Limit: 100}
	for {
		p, err := r.Events(t.Context(), catalog.AllBays(), uid, req)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range p.Items {
			if it.Position != seen || *it.Event.ContentText != text(int(seen)) {
				t.Fatalf("position %d: got %d %.30q", seen, it.Position, *it.Event.ContentText)
			}
			seen++
		}
		if p.End {
			return seen
		}
		req = EventRequest{Limit: 100, Cursor: p.NextCursor}
	}
}

// A session of several compressed frames pages through by cursor and
// by position across every frame boundary, and so does a plain file an
// older release wrote, whose cursors still carry byte offsets
// (TKT-01M3K45MX).
func TestEventsPageAcrossFramesAndPlainFiles(t *testing.T) {
	s := lake(t)
	uid := ingest(t, s, "frames")
	text := func(i int) string { return fmt.Sprintf("line %d %s", i, strings.Repeat("padding ", 120)) }
	evs := events(3000, text)
	publish(t, s, uid, evs)
	if _, err := os.Stat(normalize.EventsPath(s.Normalized, uid)); err != nil {
		t.Fatal(err)
	}
	r := NewReader(s.Catalog, s.Normalized)
	if n := pageAll(t, r, uid, text); n != 3000 {
		t.Fatalf("saw %d", n)
	}
	for _, from := range []int64{0, 1, 999, 1500, 2998} {
		p, err := r.Events(t.Context(), catalog.AllBays(), uid, EventRequest{From: from, Limit: 3})
		if err != nil || p.Items[0].Position != from || *p.Items[0].Event.ContentText != text(int(from)) {
			t.Fatalf("from %d: %v", from, err)
		}
	}
	ex, err := r.Excerpt(t.Context(), catalog.AllBays(), uid, ExcerptRequest{From: 2500, Count: 2})
	if err != nil || ex.From != 2500 || !strings.Contains(ex.Text, "line 2500 ") {
		t.Fatalf("excerpt: %v", err)
	}

	// The same events as an older release stored them.
	var plain strings.Builder
	if err := normalize.WriteJSONL(&plain, evs); err != nil {
		t.Fatal(err)
	}
	if err := normalize.RemoveEvents(s.Normalized, uid); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Normalized, uid+normalize.LegacyEventsExt), []byte(plain.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := pageAll(t, r, uid, text); n != 3000 {
		t.Fatalf("plain file: saw %d", n)
	}
}
