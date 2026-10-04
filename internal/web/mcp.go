package web

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

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
// The endpoint keeps no state and serves both protocol eras: a client
// that opens with initialize (2025-03-26 to 2025-11-25), and one that
// names its version in every request's _meta (2026-07-28). It answers
// every request with one JSON body, never an event stream.

const (
	mcpPath = "/api/read/v1/mcp"
	// mcpModern is the per-request-metadata version served.
	mcpModern = "2026-07-28"
	// mcpBodyMax caps one request. Arguments are short: a query of at
	// most 1 KiB and a cursor of at most 2 KiB.
	mcpBodyMax = 64 << 10
)

// mcpLegacy are the initialize-era versions served, newest first.
var mcpLegacy = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

// JSON-RPC and MCP error codes.
const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcNoMethod       = -32601
	rpcInvalidParams  = -32602
	rpcInternal       = -32603
	mcpHeaderMismatch = -32020
	mcpBadVersion     = -32022
)

const mcpInstructions = "Recall past agent sessions stored in this lampi lake, from every machine and project that uploaded. " +
	"Start with search, by literal text, by filters such as event_type, tool and time, or both. " +
	"Read around a hit with read_events, from a few positions before the hit's position. " +
	"Use copy_excerpt for a span as plain text to paste into a new session. " +
	"Every hit and event carries a link that opens it in the lake's web viewer."

var mcpServerInfo = map[string]string{"name": "terva-lampi", "title": "lampi session lake", "version": "1"}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcParams struct {
	Meta map[string]json.RawMessage `json:"_meta"`
	// Name and Arguments are a tools/call's; ProtocolVersion is an
	// initialize's.
	Name            string          `json:"name"`
	Arguments       json.RawMessage `json:"arguments"`
	ProtocolVersion string          `json:"protocolVersion"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *Server) mcpRoutes(m *http.ServeMux) {
	if s.reg == nil || s.events == nil {
		return
	}
	// Not behind Guard: the bearer token is the only credential it takes.
	m.HandleFunc("POST "+mcpPath, s.mcp)
	m.HandleFunc(mcpPath, func(w http.ResponseWriter, r *http.Request) {
		// Earlier revisions opened a stream with GET and ended a
		// session with DELETE. Neither exists here.
		w.Header().Set("Allow", "POST")
		apiError(w, http.StatusMethodNotAllowed, "method_not_allowed")
	})
}

func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	now := s.now()
	t, ok := s.tokenFor(w, r, catalog.PermEventsRead, "lampi-mcp", now)
	if !ok {
		return
	}
	// A browser on another site cannot send the token, but the transport
	// requires the check against DNS rebinding all the same.
	if o := r.Header.Get("Origin"); o != "" && o != s.origin {
		rpcReply(w, http.StatusForbidden, nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "origin not allowed"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpBodyMax))
	if err != nil {
		rpcReply(w, http.StatusRequestEntityTooLarge, nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "request too large"})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		// An array is a batch, which no served version allows.
		rpcReply(w, http.StatusBadRequest, nil, nil, &rpcError{Code: rpcParseError, Message: "body is not one JSON-RPC message"})
		return
	}
	// An id is a string or a number; a notification has none.
	if req.JSONRPC != "2.0" || req.Method == "" || (req.ID != nil && !validID(req.ID)) {
		rpcReply(w, http.StatusBadRequest, nil, nil, &rpcError{Code: rpcInvalidRequest, Message: "not a JSON-RPC 2.0 request"})
		return
	}
	var p rpcParams
	if len(req.Params) > 0 && json.Unmarshal(req.Params, &p) != nil {
		rpcReply(w, http.StatusBadRequest, req.ID, nil, &rpcError{Code: rpcInvalidParams, Message: "params is not an object"})
		return
	}
	// A notification's version and headers are checked as a request's
	// are (review 2105).
	modern, status, rerr := mcpVersion(r, req.Method, p)
	if rerr != nil {
		rpcReply(w, status, req.ID, nil, rerr)
		return
	}
	if req.ID == nil {
		// A notification, such as notifications/initialized. None needs
		// an answer.
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result map[string]any
	switch req.Method {
	case "initialize":
		version := mcpLegacy[0]
		if slices.Contains(mcpLegacy, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		result = map[string]any{"protocolVersion": version, "capabilities": mcpCapabilities(), "serverInfo": mcpServerInfo, "instructions": mcpInstructions}
	case "server/discover":
		result = map[string]any{"supportedVersions": append([]string{mcpModern}, mcpLegacy...), "capabilities": mcpCapabilities(),
			"_meta": map[string]any{"io.modelcontextprotocol/serverInfo": mcpServerInfo}, "instructions": mcpInstructions}
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": mcpTools}
	case "tools/call":
		result, rerr = s.mcpCall(r, t, now, p)
		if rerr != nil {
			rpcReply(w, http.StatusOK, req.ID, nil, rerr)
			return
		}
	default:
		status = http.StatusOK
		if modern {
			status = http.StatusNotFound
		}
		rpcReply(w, status, req.ID, nil, &rpcError{Code: rpcNoMethod, Message: "method not found: " + req.Method})
		return
	}
	if modern {
		result["resultType"] = "complete"
	}
	rpcReply(w, http.StatusOK, req.ID, result, nil)
}

// validID reports whether id is a JSON string or number.
func validID(id json.RawMessage) bool {
	var v any
	if json.Unmarshal(id, &v) != nil {
		return false
	}
	switch v.(type) {
	case string, float64:
		return true
	}
	return false
}

func mcpCapabilities() map[string]any {
	return map[string]any{"tools": map[string]any{}}
}

// mcpVersion checks the request's protocol version. A request that
// names one in _meta is modern: its headers must repeat the version,
// the method and a tool's name, and the version must be mcpModern. Any
// other request, initialize included, is from the initialize era, whose
// header, when sent, must name a version served; none means 2025-03-26.
func mcpVersion(r *http.Request, method string, p rpcParams) (modern bool, status int, _ *rpcError) {
	header := r.Header.Get("MCP-Protocol-Version")
	raw, modern := p.Meta["io.modelcontextprotocol/protocolVersion"]
	if !modern {
		// initialize names the version it wants in its params, and gets
		// the newest served one when that is not served; a header it
		// sends is checked like any other request's (review 2112).
		if header == "" || slices.Contains(mcpLegacy, header) {
			return false, 0, nil
		}
		if header == mcpModern {
			return false, http.StatusBadRequest, &rpcError{Code: mcpHeaderMismatch, Message: "MCP-Protocol-Version names " + mcpModern + " but _meta names no version"}
		}
		return false, http.StatusBadRequest, unsupported(header)
	}
	var version string
	if json.Unmarshal(raw, &version) != nil {
		return true, http.StatusBadRequest, &rpcError{Code: rpcInvalidParams, Message: "_meta protocol version is not a string"}
	}
	mismatch := func(name, header, body string) (bool, int, *rpcError) {
		return true, http.StatusBadRequest, &rpcError{Code: mcpHeaderMismatch, Message: fmt.Sprintf("header mismatch: %s header %q does not match body value %q", name, header, body)}
	}
	if header != version {
		return mismatch("MCP-Protocol-Version", header, version)
	}
	if h := r.Header.Get("Mcp-Method"); h != method {
		return mismatch("Mcp-Method", h, method)
	}
	if method == "tools/call" {
		if h := headerValue(r.Header.Get("Mcp-Name")); h != p.Name {
			return mismatch("Mcp-Name", h, p.Name)
		}
	}
	if version != mcpModern {
		return true, http.StatusBadRequest, unsupported(version)
	}
	return true, 0, nil
}

func unsupported(requested string) *rpcError {
	return &rpcError{Code: mcpBadVersion, Message: "unsupported protocol version",
		Data: map[string]any{"supported": append([]string{mcpModern}, mcpLegacy...), "requested": requested}}
}

// headerValue decodes the transport's base64 form of a header value,
// =?base64?...?=, and returns any other value as it is.
func headerValue(v string) string {
	inner, ok := strings.CutPrefix(v, "=?base64?")
	if inner, ok2 := strings.CutSuffix(inner, "?="); ok && ok2 {
		if b, err := base64.StdEncoding.DecodeString(inner); err == nil {
			return string(b)
		}
	}
	return v
}

func rpcReply(w http.ResponseWriter, status int, id json.RawMessage, result any, rerr *rpcError) {
	if id == nil {
		id = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rerr})
}

// mcpCall runs one tool. A bad argument or a failed read is a tool
// result with isError set and the browser route's error body, so the
// agent can correct itself. An unknown tool, or an audit line that
// cannot be queued, is a protocol error.
func (s *Server) mcpCall(r *http.Request, t catalog.ReadToken, now time.Time, p rpcParams) (map[string]any, *rpcError) {
	i := slices.IndexFunc(mcpTools, func(tool mcpTool) bool { return tool.Name == p.Name })
	q, err := mcpQuery(p.Arguments)
	// The session is a path segment of the events and excerpt routes.
	// Search has no session filter yet, so there it stays an argument
	// its parser refuses, rather than being dropped (review 2103).
	var uid string
	if p.Name != "search" {
		uid = q.Get("session_uid")
		q.Del("session_uid")
	}
	ctx := r.Context()
	lake := s.reg.Lake()
	// Every call is audited, a refused one too, and the event is durable
	// in the outbox before a result leaves, as an event stream's is.
	if err := lake.Catalog.QueueAudit(ctx, now, audit.Event{Kind: audit.EventsRead, Actor: "token:" + t.ID + " (" + t.Label + ")", Detail: mcpAuditDetail(i, uid, q)}); err != nil {
		s.logError(r, "queueing an MCP call's audit line failed", err)
		return nil, &rpcError{Code: rpcInternal, Message: "audit_failed"}
	}
	if err := lake.Catalog.FlushAudit(ctx, lake.Dir); err != nil {
		s.logError(r, "an MCP call's audit line stays queued", err)
	}
	if err := lake.Catalog.TouchReadToken(ctx, t.ID, now); err != nil {
		s.logError(r, "recording a read token's use failed", err)
	}
	if i < 0 {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown tool: " + p.Name}
	}
	if p.Name != "search" && uid == "" {
		err = recall.ErrInvalid
	}
	if err != nil {
		return toolError(err), nil
	}
	scope, err := s.catalog.ReadTokenScope(ctx, t)
	if err != nil {
		s.logError(r, "reading a read token's bays failed", err)
		return toolError(err), nil
	}
	ctx, cancel := readContext(r)
	defer cancel()
	var v any
	switch p.Name {
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
			s.logError(r, "an MCP tool call failed", err)
		}
		return toolError(err), nil
	}
	text, err := json.Marshal(v)
	if err != nil {
		return toolError(err), nil
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "isError": false}, nil
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
}

func toolError(err error) map[string]any {
	_, body := failure(err)
	out := map[string]any{"hint": mcpHints[body["error"]]}
	for k, v := range body {
		out[k] = v
	}
	text, _ := json.Marshal(out)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "isError": true}
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
	props, _ := tool.InputSchema["properties"].(map[string]any)
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

// mcpTool is one tool as tools/list describes it.
type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}

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

var readOnly = map[string]any{"readOnlyHint": true, "openWorldHint": false}

var mcpTools = []mcpTool{
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
