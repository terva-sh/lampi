// Package baydoctor provides advisory session placement independently of capture
// allowlist health. It reads metadata only; a mapping is policy, not provenance.
package baydoctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

const Version = "mapped-metadata-v1"

var ErrPolicy = errors.New("bay doctor: invalid policy or request")

type Mapping struct {
	ID           string              `json:"id"`
	Match        config.ProjectMatch `json:"match"`
	Harness      string              `json:"harness,omitempty"`
	Destinations []string            `json:"destinations"`
	Proposed     []string            `json:"proposed_bays,omitempty"`
	// Alternative names a mutually exclusive choice. Different matching mappings
	// in this group require a person to resolve the conflict.
	Alternative string `json:"alternative_group,omitempty"`
	Reason      string `json:"reason"`
}
type Ignore struct {
	FindingID string    `json:"finding_id"`
	Scope     string    `json:"scope"`
	Version   string    `json:"version"`
	Until     time.Time `json:"until"`
}
type Policy struct {
	Mappings []Mapping `json:"mappings"`
	Ignore   []Ignore  `json:"ignore,omitempty"`
}
type Request struct {
	Policy Policy                    `json:"policy"`
	Filter *catalog.DiscoveryRequest `json:"filter,omitempty"`
	UIDs   []string                  `json:"session_uids,omitempty"`
	Bays   string                    `json:"bays,omitempty"`
}
type Finding struct {
	ID           string                       `json:"id"`
	Code         string                       `json:"code"`
	Version      string                       `json:"heuristic_version"`
	UID          string                       `json:"session_uid"`
	Current      []catalog.BayReference       `json:"current_memberships"`
	Destinations []string                     `json:"destinations"`
	Proposed     []string                     `json:"proposed_bays"`
	Evidence     []string                     `json:"evidence"`
	Uncertainty  string                       `json:"uncertainty"`
	Status       string                       `json:"status"`
	Preview      *catalog.BayMigrationRequest `json:"preview_request,omitempty"`
}
type Abstention struct {
	UID  string `json:"session_uid"`
	Code string `json:"code"`
}
type Report struct {
	Version       string       `json:"heuristic_version"`
	Scope         string       `json:"scope"`
	AsOf          time.Time    `json:"as_of"`
	Complete      bool         `json:"complete"`
	Inspected     int          `json:"inspected"`
	Unmatched     int          `json:"unmatched"`
	AlreadyPlaced int          `json:"already_placed"`
	Suppressed    int          `json:"suppressed"`
	Findings      []Finding    `json:"findings"`
	Abstentions   []Abstention `json:"abstentions"`
	Warnings      []string     `json:"warnings"`
}

func DecodePolicy(raw []byte) (Policy, error) {
	var p Policy
	if len(raw) > 64<<10 {
		return p, ErrPolicy
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return p, ErrPolicy
	}
	return p, Validate(p)
}
func Validate(p Policy) error {
	if len(p.Mappings) > 20 || len(p.Ignore) > 500 {
		return ErrPolicy
	}
	seen := map[string]bool{}
	for _, m := range p.Mappings {
		if m.ID == "" || len(m.ID) > 128 || seen[m.ID] || m.Match.Empty() || len(m.Destinations) == 0 || len(m.Destinations) > 20 || len(m.Proposed) > 20 || len(m.Reason) > 1024 || strings.TrimSpace(m.Reason) == "" || len(m.Alternative) > 128 {
			return ErrPolicy
		}
		seen[m.ID] = true
		if m.Harness != "" && !slices.Contains([]string{"terva", "codex", "claude", "cursor", "cursor-cli", "opencode", "grok", "grokbot"}, m.Harness) {
			return ErrPolicy
		}
		if err := (config.Projects{Allow: []config.ProjectMatch{m.Match}}).Validate(); err != nil {
			return ErrPolicy
		}
		// A remote or harness alone does not separate purposes within a repository.
		if m.Match.CWDPrefix == "" && m.Match.CWDHash == "" && m.Match.CWDGlob == "" {
			return ErrPolicy
		}
		for _, ref := range m.Destinations {
			if ref == "" || len(ref) > 128 || strings.Contains(ref, ",") || strings.TrimSpace(ref) != ref {
				return ErrPolicy
			}
		}
		for _, name := range m.Proposed {
			if !catalog.ValidBayName(name) || !slices.Contains(m.Destinations, name) {
				return ErrPolicy
			}
		}
	}
	for _, i := range p.Ignore {
		if i.FindingID == "" || i.Scope == "" || i.Version != Version || i.Until.IsZero() {
			return ErrPolicy
		}
	}
	return nil
}

// Recommend returns a bounded report of reachable metadata. Truncation is
// explicit and never supplies a preview handoff. No content or device inventory
// is read, and this report is never consumed by profiledoctor.ApplySafe.
func Recommend(ctx context.Context, c *catalog.Catalog, scope catalog.Scope, req Request, now time.Time) (Report, error) {
	if err := Validate(req.Policy); err != nil {
		return Report{}, err
	}
	if req.Filter != nil && (req.Filter.Cursor != "" || req.Filter.Limit != 0 || len(req.UIDs) > 0) || len(req.UIDs) > catalog.MigrationMaxSessions {
		return Report{}, ErrPolicy
	}
	if req.Bays != "" {
		var err error
		scope, err = c.SelectReadBays(ctx, scope, req.Bays)
		if err != nil {
			return Report{}, err
		}
	}
	if len(req.UIDs) > 0 {
		for _, uid := range req.UIDs {
			if uid == "" || len(uid) > 128 {
				return Report{}, ErrPolicy
			}
		}
		scope = scope.OnlySessions(req.UIDs)
	}
	filter := catalog.DiscoveryRequest{}
	if req.Filter != nil {
		filter = *req.Filter
	}
	filter.Limit = 200
	var sessions []catalog.SessionMetadata
	complete := false
	for {
		page, err := c.DiscoverSessions(ctx, scope, filter)
		if err != nil {
			return Report{}, err
		}
		sessions = append(sessions, page.Items...)
		if len(sessions) > catalog.MigrationMaxSessions {
			sessions = sessions[:catalog.MigrationMaxSessions]
			break
		}
		if page.NextCursor == "" {
			complete = true
			break
		}
		filter.Cursor = page.NextCursor
	}
	// Resolve under caller scope first. Unknown and inaccessible are deliberately
	// identical, and proposed names remain hypothetical even if hidden bays exist.
	resolved := map[string]string{}
	for _, m := range req.Policy.Mappings {
		for _, ref := range m.Destinations {
			if slices.Contains(m.Proposed, ref) {
				continue
			}
			if _, err := c.SelectReadBays(ctx, scope, ref); err != nil {
				if errors.Is(err, catalog.ErrBaySelection) {
					continue
				}
				return Report{}, err
			}
			b, err := c.ResolveBay(ctx, ref)
			if err != nil {
				return Report{}, err
			}
			resolved[ref] = b.ID
		}
	}
	return Evaluate(sessions, req.Policy, resolved, scope.Fingerprint(), complete, now), nil
}

// Evaluate is pure so labeled fixtures can compare decisions independently of
// the catalog. resolved contains only authorized destination IDs.
func Evaluate(sessions []catalog.SessionMetadata, p Policy, resolved map[string]string, scope string, complete bool, now time.Time) Report {
	r := Report{Version: Version, Scope: scope, AsOf: now, Complete: complete, Findings: []Finding{}, Abstentions: []Abstention{}, Warnings: []string{"Advisory user-mapped metadata only; workflow provenance is unavailable and content was not inspected. Review purpose before previewing. Historical suggestions install no future route, create no bay and grant no access."}}
	if len(p.Mappings) == 0 {
		r.Warnings = append(r.Warnings, "No destination policy supplied; no purpose or bay names were guessed.")
	}
	if !complete {
		r.Warnings = append(r.Warnings, "Inventory exceeds 500 sessions or changed while paging; narrow the request. No preview handoffs are supplied for an incomplete report.")
	}
	for _, s := range sessions {
		r.Inspected++
		if s.MetadataTruncated {
			r.Abstentions = append(r.Abstentions, Abstention{s.UID, "incomplete_metadata"})
			continue
		}
		var matches []Mapping
		groups := map[string]int{}
		for _, m := range p.Mappings {
			if m.Harness != "" && m.Harness != s.Harness {
				continue
			}
			if !(config.Projects{Allow: []config.ProjectMatch{m.Match}}).Permitted(config.ProjectID{CWD: s.CWD, CWDHash: s.CWDHash, GitRemote: s.GitRemote}) {
				continue
			}
			matches = append(matches, m)
			if m.Alternative != "" {
				groups[m.Alternative]++
			}
		}
		if len(matches) == 0 {
			r.Unmatched++
			continue
		}
		conflict := false
		for _, n := range groups {
			if n > 1 {
				conflict = true
			}
		}
		if conflict {
			r.Abstentions = append(r.Abstentions, Abstention{s.UID, "conflicting_alternatives"})
			continue
		}
		f := Finding{Code: "bay_destination_mapping", Version: Version, UID: s.UID, Current: s.Bays, Destinations: []string{}, Proposed: []string{}, Evidence: []string{}, Uncertainty: "Purpose is unverified: metadata matches the user's mapping, not recorded workflow provenance. Inspect opening prompts and development controls; related sessions are never included automatically.", Status: "suggestion"}
		for _, m := range matches {
			match, _ := json.Marshal(m.Match)
			f.Evidence = append(f.Evidence, m.ID+": "+string(match)+"; "+m.Reason)
			for _, ref := range m.Destinations {
				if slices.Contains(m.Proposed, ref) {
					f.Destinations = append(f.Destinations, ref)
					f.Proposed = append(f.Proposed, ref)
					continue
				}
				id := resolved[ref]
				if id == "" {
					f.Status = "unknown_or_inaccessible_destination"
					f.Destinations = append(f.Destinations, ref)
					continue
				}
				if !slices.ContainsFunc(s.Bays, func(b catalog.BayReference) bool { return b.ID == id }) {
					f.Destinations = append(f.Destinations, id)
				}
			}
		}
		f.Destinations = unique(f.Destinations)
		f.Proposed = unique(f.Proposed)
		f.Evidence = unique(f.Evidence)
		if len(f.Destinations) == 0 {
			r.AlreadyPlaced++
			continue
		}
		raw, _ := json.Marshal([]any{Version, s.UID, f.Destinations, f.Proposed, f.Evidence})
		sum := sha256.Sum256(raw)
		f.ID = hex.EncodeToString(sum[:])
		ignored := slices.ContainsFunc(p.Ignore, func(i Ignore) bool {
			return i.FindingID == f.ID && i.Scope == scope && i.Version == Version && i.Until.After(now)
		})
		if ignored {
			r.Suppressed++
			continue
		}
		if !complete {
			f.Status = "incomplete_inventory"
		} else if f.Status == "suggestion" {
			f.Preview = &catalog.BayMigrationRequest{UIDs: []string{s.UID}, Add: f.Destinations, Proposed: f.Proposed, Reason: strings.Join(f.Evidence, "; ")}
			if len(f.Preview.Reason) > 1024 {
				f.Preview.Reason = "Doctor " + Version + " finding " + f.ID + "; inspect the report's full mapping evidence"
			}
		}
		r.Findings = append(r.Findings, f)
	}
	return r
}
func unique(s []string) []string { slices.Sort(s); return slices.Compact(s) }
