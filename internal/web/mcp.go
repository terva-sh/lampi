package web

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

// The MCP endpoint (TKT-01M3FPWCFS). An agent on the owner's machines
// recalls past sessions from every machine that uploaded, with a read
// token holding events:read. Each tool is a browser route over the
// shared query layer: its arguments are that route's query parameters,
// parsed by the same function, and its result is that route's JSON
// body. Links are made absolute, so an agent can hand one to a person.
//
// The protocol is the official Go SDK's (TKT-01M44DVPVX). Its server
// keeps no state, answers every request with one JSON body, and serves
// both eras: a client that opens with initialize, and one that names
// its version in every request's _meta (2026-07-28). The token check,
// the Origin check, the audit and the tools stay lampi's;
// docs/web-api.md says where they depart from the SDK's defaults.

const (
	mcpPath = "/api/read/v1/mcp"
	// mcpBodyMax caps one request. Arguments are short: a query of at
	// most 1 KiB and a cursor of at most 2 KiB.
	mcpBodyMax = 64 << 10
)

const mcpInstructions = "Recall past agent sessions stored in this lampi lake, from every machine and project that uploaded. " +
	"Start with search, by literal text, by filters such as event_type, tool and time, or both. " +
	"Read around a hit with read_events, from a few positions before the hit's position. " +
	"Use copy_excerpt for a span as plain text to paste into a new session. " +
	"Every hit and event carries a link that opens it in the lake's web viewer."

// mcpCaller is the request a tool call arrived on and its read token.
// The gate puts it in the request's context for the audit and the
// tools, which the SDK calls with that context.
type mcpCaller struct {
	r   *http.Request
	t   catalog.ReadToken
	now time.Time
}

type mcpCallerKey struct{}

func (s *Server) mcpRoutes(m *http.ServeMux) {
	if s.reg == nil || s.events == nil {
		return
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "terva-lampi", Title: "lampi session lake", Version: "1"}, &mcp.ServerOptions{
		Instructions: mcpInstructions,
		// Tools only, and no list_changed, because the list is fixed.
		// The SDK's default adds logging, which 2026-07-28 deprecates.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})
	srv.AddReceivingMiddleware(s.mcpAudit(&mcpLimits{by: map[string]*rate.Limiter{}}))
	for _, tool := range mcpTools {
		srv.AddTool(tool, s.mcpCall)
	}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: mcpBodyMax,
		// The SDK refuses a request that reaches a loopback address with
		// another Host. Behind a proxy that is every request, so the
		// Origin check in mcpGate stands in for it.
		DisableLocalhostProtection: true,
	})
	// Not behind Guard: the bearer token is the only credential it takes.
	m.Handle("POST "+mcpPath, s.mcpGate(h))
	m.HandleFunc(mcpPath, func(w http.ResponseWriter, r *http.Request) {
		// Earlier revisions opened a stream with GET and ended a
		// session with DELETE. Neither exists here.
		w.Header().Set("Allow", "POST")
		apiError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	})
}

// mcpGate admits a request to the SDK's handler: a read token holding
// events:read, and no foreign Origin.
func (s *Server) mcpGate(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		now := s.now()
		t, ok := s.tokenFor(w, r, catalog.PermEventsRead, "lampi-mcp", now)
		if !ok {
			return
		}
		// A browser on another site cannot send the token, but the
		// transport requires the check against DNS rebinding all the
		// same. Its body may be a JSON-RPC error with no id.
		if o := r.Header.Get("Origin"); o != "" && o != s.origin {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "error": map[string]any{"code": jsonrpc.CodeInvalidRequest, "message": "origin not allowed"}})
			return
		}
		// The SDK compares Mcp-Name to the body as sent. The transport
		// says a server decodes the base64 form first.
		if name, ok := decodeHeader(r.Header.Get("Mcp-Name")); ok {
			r.Header.Set("Mcp-Name", name)
		}
		ctx := context.WithValue(r.Context(), mcpCallerKey{}, mcpCaller{r: r, t: t, now: now})
		h.ServeHTTP(&noStore{ResponseWriter: w}, r.WithContext(ctx))
	}
}

// noStore keeps an answer out of every cache, as for all authenticated
// data. The SDK sets Cache-Control: no-cache, which lets a cache keep a
// copy, so the header is replaced as the status is written.
type noStore struct {
	http.ResponseWriter
	wrote bool
}

func (w *noStore) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *noStore) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *noStore) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// decodeHeader decodes the transport's base64 form of a header value,
// =?base64?...?=. ok is false for any other value.
func decodeHeader(v string) (string, bool) {
	inner, ok := strings.CutPrefix(v, "=?base64?")
	if inner, ok2 := strings.CutSuffix(inner, "?="); ok && ok2 {
		if b, err := base64.StdEncoding.DecodeString(inner); err == nil {
			return string(b), true
		}
	}
	return "", false
}

// Tool calls a read token may make (TKT-01M445H1). The tools
// specification asks a server to rate-limit them, and each call syncs
// an audit line. An agent's burst of parallel searches passes, and a
// runaway loop is held to two synced writes a second.
const (
	mcpRate  = 2  // calls per second, refilled
	mcpBurst = 30 // calls at once
)

// errToolRate is a tool call over its token's limit.
var errToolRate = errors.New("mcp: too many tool calls with this token")

// mcpLimits holds a limiter per read token, by id. An admin mints the
// tokens, so the map stays small. A limiter takes the gate's time,
// which carries the monotonic clock, so a step in wall time does not
// refill it.
type mcpLimits struct {
	mu sync.Mutex
	by map[string]*rate.Limiter
}

func (l *mcpLimits) allow(token string, now time.Time) bool {
	l.mu.Lock()
	lim, ok := l.by[token]
	if !ok {
		lim = rate.NewLimiter(mcpRate, mcpBurst)
		l.by[token] = lim
	}
	l.mu.Unlock()
	return lim.AllowN(now, 1)
}

// mcpAudit records every tools/call before the SDK looks the tool up,
// so a call to an unknown tool, which the SDK refuses before a tool
// runs, is audited too. The event is durable in the outbox before a
// result leaves, as an event stream's is. A call over its token's limit
// is refused first, unaudited: the limit is what bounds the synced
// writes, as on the open routes.
func (s *Server) mcpAudit(limits *mcpLimits) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			c, ok := ctx.Value(mcpCallerKey{}).(mcpCaller)
			if !ok {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "mcp: a tool call without its caller"}
			}
			if !limits.allow(c.t.ID, c.now) {
				return toolError(errToolRate), nil
			}
			// A call naming no tool is audited as one to an unknown tool.
			// One with no params at all is refused by the SDK's transport
			// before it gets here; it names nothing and reads nothing.
			var name string
			var args json.RawMessage
			if p, _ := req.GetParams().(*mcp.CallToolParamsRaw); p != nil {
				name, args = p.Name, p.Arguments
			}
			i := slices.IndexFunc(mcpTools, func(tool *mcp.Tool) bool { return tool.Name == name })
			uid, q, _ := mcpArgs(name, args)
			lake := s.reg.Lake()
			if err := lake.Catalog.QueueAudit(ctx, c.now, audit.Event{Kind: audit.EventsRead, Actor: "token:" + c.t.ID + " (" + c.t.Label + ")", Detail: mcpAuditDetail(i, uid, q)}); err != nil {
				s.logError(c.r, "queueing an MCP call's audit line failed", err)
				// A plain error reaches the wire with code 0.
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "audit_failed"}
			}
			if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
				s.logError(c.r, "an MCP call's audit line stays queued", err)
			}
			if err := lake.Catalog.TouchReadToken(ctx, c.t.ID, c.now); err != nil {
				s.logError(c.r, "recording a read token's use failed", err)
			}
			return next(ctx, method, req)
		}
	}
}

// mcpArgs is a call's arguments as its browser route's query, and the
// session it names. The session is a path segment of the events and
// excerpt routes. Search has no session filter yet, so there it stays
// an argument its parser refuses, rather than being dropped (review
// 2103).
func mcpArgs(tool string, args json.RawMessage) (uid string, q url.Values, err error) {
	q, err = mcpQuery(args)
	if tool != "search" {
		uid = q.Get("session_uid")
		q.Del("session_uid")
	}
	return uid, q, err
}

// mcpCall runs one tool, already audited. A bad argument or a failed
// read is a tool result with isError set and the browser route's error
// body, so the agent can correct itself.
func (s *Server) mcpCall(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	c, ok := ctx.Value(mcpCallerKey{}).(mcpCaller)
	if !ok {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "mcp: a tool call without its caller"}
	}
	name := req.Params.Name
	uid, q, err := mcpArgs(name, req.Params.Arguments)
	if name != "search" && uid == "" {
		err = recall.ErrInvalid
	}
	if err != nil {
		return toolError(err), nil
	}
	// The read runs under the context the SDK gave the call, so a call
	// it cancels stops reading; c.r is only for the log (review on #195).
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	scope, err := s.catalog.ReadTokenScope(ctx, c.t)
	if err != nil {
		s.logError(c.r, "reading a read token's bays failed", err)
		return toolError(err), nil
	}
	var v any
	switch name {
	case "search":
		page, err2 := s.runSearch(ctx, scope, q)
		for i := range page.Items {
			page.Items[i].Link = s.origin + page.Items[i].Link
		}
		v, err = page, err2
	case "read_events":
		page, err2 := s.eventPage(ctx, scope, uid, q)
		for i := range page.Items {
			page.Items[i].Link = s.origin + page.Items[i].Link
		}
		v, err = page, err2
	case "copy_excerpt":
		v, err = s.excerpt(ctx, scope, uid, q)
	}
	if err != nil {
		if status, _ := failure(err); status >= 500 {
			s.logError(c.r, "an MCP tool call failed", err)
		}
		return toolError(err), nil
	}
	text, err := json.Marshal(v)
	if err != nil {
		return toolError(err), nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil
}

// mcpQuery turns a tool's arguments into the query parameters of its
// browser route: a string as it is, an integer in decimal, and a
// boolean as true or false. A null is left out. Anything else is
// ErrInvalid.
func mcpQuery(args json.RawMessage) (url.Values, error) {
	q := url.Values{}
	if len(args) == 0 || string(args) == "null" {
		return q, nil
	}
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return q, recall.ErrInvalid
	}
	for k, v := range m {
		switch v := v.(type) {
		case nil:
		case string:
			q.Set(k, v)
		case bool:
			q.Set(k, strconv.FormatBool(v))
		case json.Number:
			n, err := v.Int64()
			if err != nil {
				return q, recall.ErrInvalid
			}
			q.Set(k, strconv.FormatInt(n, 10))
		default:
			return q, recall.ErrInvalid
		}
	}
	return q, nil
}

// mcpHints say what each error code asks of the agent.
var mcpHints = map[string]string{
	"invalid_request":        "An argument is missing, unknown, out of range or of the wrong type. Text needs at least 3 characters unless a filter is given; limits run from 1 to 200; dates are RFC 3339 or YYYY-MM-DD in UTC; a cursor goes with the same other arguments that produced it.",
	"not_found":              "No session with this uid is readable with this token.",
	"generation_changed":     "The session was normalized again after gen or the cursor was read. Call again without gen and cursor.",
	"transcript_unavailable": "The session has no readable normalized transcript; state says why.",
	"search_unavailable":     "This lake has no search index.",
	"read_unavailable":       "The read ran out of time. Narrow it with filters, a project or a time range.",
	"read_failed":            "The lake could not read. Try again later.",
	"rate_limited":           "Too many tool calls with this token. Wait a second and call again: after a burst of " + strconv.Itoa(mcpBurst) + " calls, the lake allows " + strconv.Itoa(mcpRate) + " a second.",
}

func toolError(err error) *mcp.CallToolResult {
	_, body := failure(err)
	out := map[string]any{"hint": mcpHints[body["error"]]}
	for k, v := range body {
		out[k] = v
	}
	text, _ := json.Marshal(out)
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}, IsError: true}
}

// mcpAuditDetail names the tool, the session and the filters of a call
// to mcpTools[i], or to an unknown tool when i is negative. Only the
// arguments the tool's schema names are recorded, so a value under a
// wrong name, which could be anything, is counted and never written.
// Search text is recorded by its length only: it says what an agent
// looked for, which can be as private as what it found. A cursor is
// recorded as present, and a session uid only when it looks like one.
func mcpAuditDetail(i int, uid string, q url.Values) string {
	if i < 0 {
		return "mcp tool=unknown"
	}
	tool := mcpTools[i]
	parts := []string{"mcp tool=" + tool.Name}
	if uid != "" {
		if !auditSafe.MatchString(uid) {
			uid = "invalid"
		}
		parts = append(parts, "session="+uid)
	}
	schema, _ := tool.InputSchema.(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	keys := make([]string, 0, len(q))
	unknown := 0
	for k := range q {
		if _, ok := props[k]; ok {
			keys = append(keys, k)
		} else {
			unknown++
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := q.Get(k)
		switch {
		case k == "q":
			v = strconv.Itoa(len(v)) + "B"
		case k == "cursor":
			v = "yes"
		case len(v) > 256:
			v = strconv.Itoa(len(v)) + "B"
		}
		parts = append(parts, k+"="+url.QueryEscape(v))
	}
	if unknown > 0 {
		parts = append(parts, "unknown_args="+strconv.Itoa(unknown))
	}
	return strings.Join(parts, " ")
}

// auditSafe matches a session uid worth recording as it is.
var auditSafe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

func intProp(description string, min, max int) map[string]any {
	p := prop("integer", description)
	p["minimum"] = min
	if max > 0 {
		p["maximum"] = max
	}
	return p
}

func enumProp(description string, values []string) map[string]any {
	p := prop("string", description)
	p["enum"] = values
	return p
}

func object(required []string, props map[string]any) map[string]any {
	o := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if required != nil {
		o["required"] = required
	}
	return o
}

// readOnly marks a tool that changes nothing and reaches only the lake.
var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)}

// mcpTools are the tools, in the order the SDK does not keep: tools/list
// sorts them by name.
var mcpTools = []*mcp.Tool{
	{
		Name:  "search",
		Title: "Search past sessions",
		Description: "Search the normalized events of past agent sessions from every machine and project that uploaded to this lake. " +
			"Give literal text, filters, or both. Hits come newest indexed first. Each names its session, generation and position, " +
			"with a snippet around the match and a link to the event in the lake's viewer. A page can hold fewer hits than limit; " +
			"continue with next_cursor while it is not empty. Read around a hit with read_events.",
		InputSchema: object(nil, map[string]any{
			"q":          prop("string", "Literal text, matched case-insensitively anywhere in an event's text. At least 3 characters. Optional when a filter is given."),
			"harness":    prop("string", "Only sessions from this harness, such as claude, codex, opencode, cursor or grok."),
			"project":    prop("string", "Only sessions in this project, by the project_id a hit carries."),
			"unlinked":   prop("boolean", "Only sessions linked to no project."),
			"since":      prop("string", "Only events recorded at or after this UTC time, as RFC 3339 or YYYY-MM-DD."),
			"until":      prop("string", "Only events recorded before this UTC time, as RFC 3339 or YYYY-MM-DD. A date alone covers that whole day."),
			"event_type": enumProp("Only events of this type.", recall.EventTypes),
			"actor":      enumProp("Only events from this actor.", recall.Actors),
			"tool":       prop("string", "Only tool calls and results of this tool, named exactly as recorded, such as Bash."),
			"tool_error": prop("boolean", "Only tool results that failed (true) or succeeded (false), as the harness recorded it."),
			"raw_type":   prop("string", "Only events whose harness record type is exactly this."),
			"limit":      intProp("Hits per page. The default is 50.", 1, recall.SearchMaxLimit),
			"cursor":     prop("string", "next_cursor from the previous page. Give the same other arguments."),
		}),
		Annotations: readOnly,
	},
	{
		Name:  "read_events",
		Title: "Read a session's events",
		Description: "Read consecutive events of one session in transcript order, for example from a few positions before a search hit. " +
			"Each event carries its position and a link. Text over 32 KiB in one event is cut and marked. A page stops at limit or about 1 MiB, " +
			"so ask for 20 to 40 events at a time. Continue with next_cursor; end is true at the last event.",
		InputSchema: object([]string{"session_uid"}, map[string]any{
			"session_uid": prop("string", "The session, as a hit's session_uid."),
			"from":        intProp("First position, counted from zero. Ignored with cursor.", 0, 0),
			"limit":       intProp("Events per page. The default is 100.", 1, recall.MaxLimit),
			"cursor":      prop("string", "next_cursor from the previous page of this session."),
			"gen":         intProp("Read only this generation, as a hit's generation. If the session was normalized again since, the call fails with generation_changed.", 0, 0),
		}),
		Annotations: readOnly,
	},
	{
		Name:  "copy_excerpt",
		Title: "Copy a span as text",
		Description: "Render up to 200 consecutive events of one session as labelled plain text, ready to paste into a new session or hand to a person. " +
			"The text field holds the excerpt. Its header names the session and project and links to the first event.",
		InputSchema: object([]string{"session_uid"}, map[string]any{
			"session_uid": prop("string", "The session, as a hit's session_uid."),
			"from":        intProp("First position, counted from zero. The default is 0.", 0, 0),
			"count":       intProp("How many events. The default is 200.", 1, recall.ExcerptMaxEvents),
			"gen":         intProp("Copy only this generation, as a hit's generation.", 0, 0),
		}),
		Annotations: readOnly,
	},
}
