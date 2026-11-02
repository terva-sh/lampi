package web

import (
	"context"
	"net/url"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

// selectedReadScope removes the adapter-level selector before parsing the
// remaining operation arguments, so HTTP and MCP share one contract.
func (s *Server) selectedReadScope(ctx context.Context, scope catalog.Scope, q url.Values) (catalog.Scope, url.Values, error) {
	values, ok := q["bays"]
	if !ok {
		return scope, q, nil
	}
	if len(values) != 1 || values[0] == "" {
		return catalog.Scope{}, q, recall.ErrInvalid
	}
	next, err := s.catalog.SelectReadBays(ctx, scope, values[0])
	if err != nil {
		return catalog.Scope{}, q, err
	}
	copy := make(url.Values, len(q))
	for k, v := range q {
		if k != "bays" {
			copy[k] = v
		}
	}
	return next, copy, nil
}

func init() {
	for _, tool := range mcpTools {
		if tool.Name == "list_bays" {
			continue
		}
		schema, _ := tool.InputSchema.(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		properties["bays"] = prop("string", "Focus on any of up to 20 comma-separated bay IDs, names or aliases. Intersects current access and other filters; shared sessions count once. Keep this selection when continuing a cursor. Omit for all reachable sessions.")
	}
}
