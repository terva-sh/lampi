package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

func init() { discoveryTools() }

func newDiscoveryTool(name, title, description string, required []string, properties map[string]any) *mcp.Tool {
	return &mcp.Tool{Name: name, Title: title, Description: description, InputSchema: object(required, properties), Annotations: readOnly}
}

// Metadata discovery shares events:read and its scope. It never returns
// manifests, raw artifacts, device configuration or grant principals.
func (s *Server) readDiscoveryRoutes(m *http.ServeMux) {
	if s.reg == nil {
		return
	}
	for _, kind := range []string{"bays", "projects", "sessions"} {
		m.HandleFunc("GET /api/read/v1/"+kind, func(w http.ResponseWriter, r *http.Request) {
			s.readTokenAnswer(w, r, "discover "+kind, func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
				return s.discover(ctx, scope, kind, "", q)
			})
		})
	}
	m.HandleFunc("GET /api/read/v1/sessions/{uid}", func(w http.ResponseWriter, r *http.Request) {
		// The audit identifies the operation, not arbitrary caller text.
		s.readTokenAnswer(w, r, "discover session", func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
			return s.discover(ctx, scope, "session", r.PathValue("uid"), q)
		})
	})
}

func (s *Server) discover(ctx context.Context, scope catalog.Scope, kind, uid string, q url.Values) (any, error) {
	if kind == "bays" && q.Has("bays") {
		return nil, recall.ErrInvalid
	}
	selected, query, selectErr := s.selectedReadScope(ctx, scope, q)
	if selectErr != nil {
		return nil, selectErr
	}
	scope, q = selected, query
	if kind == "session" {
		if len(q) > 0 {
			return nil, recall.ErrInvalid
		}
		return s.catalog.DiscoverSession(ctx, scope, uid)
	}
	r, err := parseDiscovery(q)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "bays":
		return s.catalog.DiscoverBays(ctx, scope, r)
	case "projects":
		return s.catalog.DiscoverProjects(ctx, scope, r)
	case "sessions":
		return s.catalog.DiscoverSessions(ctx, scope, r)
	default:
		return nil, recall.ErrInvalid
	}
}

func parseDiscovery(q url.Values) (catalog.DiscoveryRequest, error) {
	r := catalog.DiscoveryRequest{UpdatedSince: q.Get("updated_since"), UpdatedUntil: q.Get("updated_until"), Harness: q.Get("harness"), Project: q.Get("project"), CWDPrefix: q.Get("cwd_prefix"), CWDHash: q.Get("cwd_hash"), Machine: q.Get("machine"), Cursor: q.Get("cursor")}
	for k, values := range q {
		switch k {
		case "updated_since", "updated_until", "harness", "project", "unlinked", "cwd_prefix", "cwd_hash", "machine", "limit", "cursor":
		default:
			return r, recall.ErrInvalid
		}
		if len(values) != 1 || values[0] == "" {
			return r, recall.ErrInvalid
		}
	}
	var err error
	if v := q.Get("unlinked"); v != "" {
		r.Unlinked, err = strconv.ParseBool(v)
		if err != nil || v != "true" && v != "false" {
			return r, recall.ErrInvalid
		}
	}
	if v := q.Get("limit"); v != "" {
		r.Limit, err = strconv.Atoi(v)
		if err != nil || r.Limit < 1 || r.Limit > 200 {
			return r, recall.ErrInvalid
		}
	}
	return r, nil
}

func discoveryTools() {
	page := func() map[string]any {
		return map[string]any{
			"limit":  intProp("Items per page, default 50. Continue until next_cursor is empty.", 1, 200),
			"cursor": prop("string", "next_cursor from the same tool with identical filters and access scope."),
		}
	}
	filters := func() map[string]any {
		p := page()
		p["bays"] = prop("string", "Any of up to 20 comma-separated visible bay IDs, names or aliases; intersects current scope.")
		p["updated_since"] = prop("string", "Only sessions whose head was updated on/after this UTC RFC3339 time or date.")
		p["updated_until"] = prop("string", "Only sessions updated before this UTC time; a date covers the whole day.")
		p["harness"] = prop("string", "Only sessions from this harness.")
		p["project"] = prop("string", "Only this project_id, discovered with list_projects.")
		p["unlinked"] = prop("boolean", "Only sessions with no project_id; useful for review companions.")
		p["cwd_prefix"] = prop("string", "Only sessions at or below this absolute directory, on a path boundary.")
		p["cwd_hash"] = prop("string", "Only sessions with this exact working-directory hash.")
		p["machine"] = prop("string", "Only sessions with provenance from this machine ID.")
		return p
	}
	projects := filters()
	delete(projects, "unlinked")
	mcpTools = append(mcpTools,
		newDiscoveryTool("list_bays", "List visible bays", "List permitted bays with counts of reachable sessions. Names and counts never reveal other bays or sessions.", nil, page()),
		newDiscoveryTool("list_projects", "List visible projects", "List projects linked to reachable stored sessions, with scoped counts. Use list_sessions with unlinked=true for sessions without a project.", nil, projects),
		newDiscoveryTool("list_sessions", "List session metadata", "Enumerate reachable stored sessions, including those without indexed transcripts. Returns directory, remote, harness, machine provenance and visible bay memberships. Filters combine with AND. Workflow provenance is null when unknown. Continue through every page before treating a selection as complete.", nil, filters()),
		newDiscoveryTool("get_session", "Inspect session metadata", "Read routing metadata for one reachable session. Reveals no raw artifacts, grants or hidden bay memberships.", []string{"session_uid"}, map[string]any{"bays": prop("string", "Read only within any selected visible bays, comma-separated."), "session_uid": prop("string", "The session_uid from discovery or a search hit.")}),
	)
}
