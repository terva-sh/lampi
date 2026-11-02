package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"terva.sh/lampi/internal/baydoctor"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/recall"
)

func init() {
	match := object(nil, map[string]any{"cwd_prefix": prop("string", "Verified directory and descendants."), "cwd_hash": prop("string", "Exact directory hash."), "cwd_glob": prop("string", "Verified directory layout."), "git_remote": prop("string", "Optional corroborating remote."), "git_remote_prefix": prop("string", "Optional corroborating remote prefix.")})
	mapping := object([]string{"id", "match", "destinations", "reason"}, map[string]any{"id": prop("string", "Stable mapping identifier."), "match": match, "harness": prop("string", "Optional capture harness."), "destinations": stringList("Compatible destination references."), "proposed_bays": stringList("Hypothetical new destination names included in destinations."), "alternative_group": prop("string", "Different matching mappings in this group are conflicting alternatives; abstain."), "reason": prop("string", "Why the operator verified this metadata layout; no transcript dumps.")})
	mappings := map[string]any{"type": "array", "maxItems": 20, "items": mapping}
	ignore := object([]string{"finding_id", "scope", "version", "until"}, map[string]any{"finding_id": prop("string", "Exact prior finding ID."), "scope": prop("string", "Exact prior report scope fingerprint."), "version": prop("string", "Heuristic version from the report."), "until": prop("string", "Explicit dismissal expiry as RFC3339.")})
	policy := object([]string{"mappings"}, map[string]any{"mappings": mappings, "ignore": map[string]any{"type": "array", "maxItems": 500, "items": ignore}})
	filter := object(nil, map[string]any{"harness": prop("string", "Exact harness."), "project": prop("string", "Exact project ID."), "unlinked": prop("boolean", "Only sessions without project IDs."), "cwd_prefix": prop("string", "Directory and descendants."), "cwd_hash": prop("string", "Exact directory hash."), "machine": prop("string", "Provenance machine ID."), "updated_since": prop("string", "Head updated on/after UTC time/date."), "updated_until": prop("string", "Head updated before UTC time or through date.")})
	mcpTools = append(mcpTools, newDiscoveryTool("recommend_bays", "Suggest bay destinations with evidence", "Read-only bay doctor using explicit user-supplied directory mappings, never guessed purpose or bay names. Shows stable findings, evidence, uncertainty, current visible placement, alternatives/abstentions and exact add-preview requests. No content, capture health, grants, routes or mutations. At most 500 sessions; incomplete reports supply no preview handoffs. Include reviewer/rater companions explicitly; directory matches can include development. Ignore feedback is request-local, scoped, versioned and expiring; persist the private policy yourself.", []string{"policy"}, map[string]any{"policy": policy, "filter": filter, "session_uids": stringList("Exact sessions; cannot combine with filter."), "bays": prop("string", "Any selected visible bays, intersected with caller scope.")}))
}
func (s *Server) recommendCall(ctx context.Context, scope catalog.Scope, raw json.RawMessage) (any, error) {
	var req baydoctor.Request
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if len(raw) > 64<<10 || d.Decode(&req) != nil || d.Decode(new(any)) != io.EOF {
		return nil, recall.ErrInvalid
	}
	// Do not turn a typo or null policy into an apparently useful report.
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields["policy"] == nil || string(fields["policy"]) == "null" {
		return nil, recall.ErrInvalid
	}
	report, err := baydoctor.Recommend(ctx, s.catalog, scope, req, s.now())
	if err == baydoctor.ErrPolicy {
		return nil, recall.ErrInvalid
	}
	return report, err
}
func (s *Server) bayDoctorRoutes(m *http.ServeMux) {
	if s.reg == nil {
		return
	}
	m.HandleFunc("POST /api/read/v1/recommend-bays", func(w http.ResponseWriter, r *http.Request) {
		s.readTokenAnswer(w, r, "bay recommendations", func(ctx context.Context, scope catalog.Scope, q url.Values) (any, error) {
			if len(q) > 0 {
				return nil, recall.ErrInvalid
			}
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpBodyMax))
			if err != nil {
				return nil, recall.ErrInvalid
			}
			return s.recommendCall(ctx, scope, raw)
		})
	})
}

type bayDoctorItem struct {
	baydoctor.Finding
	RequestJSON string
}
type bayDoctorView struct {
	baydoctor.Report
	Items []bayDoctorItem
}

func makeBayDoctorView(report baydoctor.Report) *bayDoctorView {
	v := &bayDoctorView{Report: report}
	for _, f := range report.Findings {
		item := bayDoctorItem{Finding: f}
		if f.Preview != nil {
			raw, _ := json.Marshal(f.Preview)
			item.RequestJSON = string(raw)
		}
		v.Items = append(v.Items, item)
	}
	return v
}
