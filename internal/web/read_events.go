package web

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

// The event stream (TKT-01M3V3JS). A read token holding events:read
// gets every normalized event in its scope that a filter keeps, as
// NDJSON, so an agent can read a slice of the lake without shell access
// to the lake host. It selects with recall.EventFilter, so the same
// filters give the same rows as terva-lampi export.

const (
	readEventsPath = "/api/read/v1/events"
	// ReadEventsEndKey is the only key of the line that ends a stream.
	// No event field holds a colon, so no event or field selection can
	// be mistaken for it. A stream without it was cut short.
	ReadEventsEndKey = "lampi:end"
)

// ReadEventsEnd is the stream's last line, under ReadEventsEndKey.
// Complete is false, with Error set, when the lake stopped reading
// partway; the rows before it are real but not all there are.
type ReadEventsEnd struct {
	Complete bool   `json:"complete"`
	Error    string `json:"error,omitempty"`
	recall.SelectStats
}

func (s *Server) readEventRoutes(m *http.ServeMux) {
	if s.reg == nil || s.events == nil {
		return
	}
	// Not behind Guard: the bearer token is the only credential it takes.
	m.HandleFunc("GET "+readEventsPath, s.readEvents)
}

func (s *Server) readEvents(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	t, ok := s.tokenFor(w, r, catalog.PermEventsRead, "lampi-events", now)
	if !ok {
		return
	}
	// ParseQuery reports what URL.Query drops silently, such as a bad
	// percent escape, so a malformed parameter is refused rather than
	// left out of the filter (review 1682).
	q, err := url.ParseQuery(r.URL.RawQuery)
	var f recall.EventFilter
	var fields recall.Fields
	if err == nil {
		f, fields, err = parseReadEvents(q)
	}
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var counter *recall.Counter
	if by := q.Get("count_by"); by != "" {
		if fields != nil {
			apiError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if counter, err = recall.NewCounter(by); err != nil {
			apiError(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	// One call must not dump the token's whole scope by accident.
	if f.IsZero() {
		apiError(w, http.StatusBadRequest, "filter_required")
		return
	}
	ctx := r.Context()
	lake := s.reg.Lake()
	// The event is durable in the outbox before a line leaves, as a raw
	// read's is. The query names filters and paths, never content.
	if err := lake.Catalog.QueueAudit(ctx, now, audit.Event{Kind: audit.EventsRead, Actor: "token:" + t.ID + " (" + t.Label + ")", Detail: "query=" + q.Encode()}); err != nil {
		s.logError(r, "queueing an event read's audit line failed", err)
		apiError(w, http.StatusInternalServerError, "audit_failed")
		return
	}
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		s.logError(r, "an event read's audit line stays queued", err)
	}
	if err := lake.Catalog.TouchReadToken(ctx, t.ID, now); err != nil {
		s.logError(r, "recording a read token's use failed", err)
	}
	reaches := func(uid string) (bool, error) {
		if !t.Allows(catalog.PermEventsRead, uid, now) {
			return false, nil
		}
		return s.catalog.ReadTokenReaches(ctx, t, uid)
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	out := newKeepaliveWriter(w, ReadEventsKeepalive)
	emit := out.line
	if counter != nil {
		emit = counter.Add
	}
	st, err := s.events.Select(ctx, f, fields, reaches, emit)
	if counter != nil && err == nil {
		// The rows are the counts, so rows says how many lines were sent.
		st.Rows = 0
		for _, row := range counter.Rows() {
			if err = out.line(row); err != nil {
				break
			}
			st.Rows++
		}
	}
	out.stop()
	if ctx.Err() != nil {
		// The client went away; there is nobody to tell.
		return
	}
	end := ReadEventsEnd{Complete: err == nil, SelectStats: st}
	switch {
	case errors.Is(err, recall.ErrTooManyValues):
		end.Error, end.Rows = "too_many_values", 0
	case err != nil:
		s.logError(r, "streaming events stopped", err)
		end.Error = "read_failed"
	}
	line, _ := json.Marshal(map[string]ReadEventsEnd{ReadEventsEndKey: end})
	if err := out.finish(line); err != nil {
		s.logError(r, "sending events stopped", err)
	}
}

// ReadEventsKeepalive is how long the stream stays silent at most. A
// selective filter can read for minutes between matches, and rows sit
// in a 64 KiB buffer, so every interval the buffer is flushed, or, when
// it is empty and nothing was sent, a blank line is. A client can then
// treat a longer silence as a dead connection (review 1686).
const ReadEventsKeepalive = 15 * time.Second

// keepaliveWriter buffers stream lines and flushes them, or a blank
// keepalive line, at least once an interval. line and the ticker share
// the buffer under mu.
type keepaliveWriter struct {
	mu   sync.Mutex
	w    http.ResponseWriter
	bw   *bufio.Writer
	sent bool
	done chan struct{}
	wg   sync.WaitGroup
}

func newKeepaliveWriter(w http.ResponseWriter, every time.Duration) *keepaliveWriter {
	k := &keepaliveWriter{w: w, bw: bufio.NewWriterSize(w, 64<<10), done: make(chan struct{})}
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-k.done:
				return
			case <-t.C:
				k.tick()
			}
		}
	}()
	return k
}

func (k *keepaliveWriter) line(b []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.bw.Write(b)
	if err := k.bw.WriteByte('\n'); err != nil {
		return err
	}
	if k.bw.Buffered() == 0 {
		k.sent = true
	}
	return nil
}

func (k *keepaliveWriter) tick() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.bw.Buffered() == 0 && !k.sent {
		k.bw.WriteByte('\n')
	}
	k.sent = false
	if k.bw.Flush() == nil {
		http.NewResponseController(k.w).Flush()
	}
}

// stop ends the ticker; finish then writes the end line.
func (k *keepaliveWriter) stop() {
	close(k.done)
	k.wg.Wait()
}

func (k *keepaliveWriter) finish(end []byte) error {
	k.bw.Write(end)
	k.bw.WriteByte('\n')
	return k.bw.Flush()
}

// parseReadEvents reads the stream's query: the web search filters,
// under the same names, and fields. An unknown or repeated parameter,
// or an empty value, is refused.
func parseReadEvents(q url.Values) (recall.EventFilter, recall.Fields, error) {
	var f recall.EventFilter
	bad := errors.New("invalid request")
	for k, v := range q {
		if len(v) != 1 || v[0] == "" {
			return f, nil, bad
		}
		switch k {
		case "harness", "project", "event_type", "actor", "tool", "tool_error", "raw_type", "since", "until", "fields", "count_by":
		default:
			return f, nil, bad
		}
	}
	f = recall.EventFilter{Harness: q.Get("harness"), Project: q.Get("project"), EventType: q.Get("event_type"),
		Actor: q.Get("actor"), ToolName: q.Get("tool"), RawType: q.Get("raw_type")}
	switch q.Get("tool_error") {
	case "":
	case "true", "false":
		v := q.Get("tool_error") == "true"
		f.ToolError = &v
	default:
		return f, nil, bad
	}
	for _, w := range []struct {
		key  string
		dest **time.Time
		end  bool
	}{{"since", &f.Since, false}, {"until", &f.Until, true}} {
		if raw := q.Get(w.key); raw != "" {
			t, err := recall.ParseWhen(raw, w.end)
			if err != nil {
				return f, nil, bad
			}
			*w.dest = &t
		}
	}
	if err := f.Validate(); err != nil {
		return f, nil, err
	}
	fields, err := recall.ParseFields(q.Get("fields"))
	if err != nil {
		return f, nil, err
	}
	return f, fields, nil
}
