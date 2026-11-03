package web

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

func normalizeReview(in []config.ReviewRule) []config.ReviewRule {
	var out []config.ReviewRule
	for _, rule := range in {
		rule.CWDPrefix = strings.TrimSpace(rule.CWDPrefix)
		if rule.CWDPrefix == "" {
			continue
		}
		rule.CWDPrefix = path.Clean(rule.CWDPrefix)
		out = append(out, rule)
	}
	return out
}

func reviewBlanks(in []config.ReviewRule) []config.ReviewRule {
	out := append([]config.ReviewRule{}, in...)
	for range min(blankRuleRows, max(0, maxRuleRows-len(in))) {
		out = append(out, config.ReviewRule{Action: "hide"})
	}
	return out
}

func readReviewRules(v url.Values) ([]config.ReviewRule, error) {
	n := 0
	var err error
	if v.Has("review_rows") {
		n, err = strconv.Atoi(v.Get("review_rows"))
	}
	if err != nil || n < 0 || n > maxFormRows {
		return nil, fmt.Errorf("review: the form's row count is not valid")
	}
	var out []config.ReviewRule
	for i := range n {
		if v.Get("remove_review") == strconv.Itoa(i) {
			continue
		}
		prefix := "review." + strconv.Itoa(i) + "."
		out = append(out, config.ReviewRule{CWDPrefix: v.Get(prefix + "cwd_prefix"), Action: v.Get(prefix + "action")})
	}
	return out, nil
}

type profileVisibility struct {
	Hides    []projectChange `json:"hides"`
	Shows    []projectChange `json:"shows"`
	Unlisted []string        `json:"unlisted"`
}

func (s *Server) reviewVisibilityReach(ctx context.Context, before, after config.Profile, devices []profileDevice) (profileVisibility, error) {
	var out profileVisibility
	hides, shows := map[catalog.ProjectKey]*projectChange{}, map[catalog.ProjectKey]*projectChange{}
	manual, err := s.catalog.HiddenProjects(ctx)
	if err != nil {
		return out, err
	}
	exact := map[catalog.ProjectKey]bool{}
	for _, h := range manual {
		exact[h.Key] = true
	}
	for _, device := range devices {
		inv, ok, err := s.catalog.DeviceInventoryOf(ctx, device.ID)
		if err != nil {
			return out, err
		}
		if !ok || inv.Inventory.Mode == protocol.InventoryStrict {
			out.Unlisted = append(out.Unlisted, device.Name)
		}
		for _, project := range inv.Inventory.Projects {
			key, ok := catalog.ProjectKeyOf(project)
			if !ok || exact[key] {
				continue
			}
			_, was := hiddenByProfile(before, project)
			_, is := hiddenByProfile(after, project)
			if was == is {
				continue
			}
			into := hides
			if !is {
				into = shows
			}
			change := into[key]
			if change == nil {
				change = &projectChange{Key: key}
				into[key] = change
			}
			if !slices.Contains(change.Devices, device.Name) {
				change.Devices = append(change.Devices, device.Name)
			}
			change.Sessions += project.Sessions
		}
	}
	out.Hides, out.Shows = sortedChanges(hides), sortedChanges(shows)
	return out, nil
}

func preserveReviewAllows(before, after []config.ProjectMatch, review config.ReviewConfig) []config.ProjectMatch {
	for _, rule := range before {
		if review.ProtectsAllow(rule) && !slices.Contains(after, rule) {
			after = append(after, rule)
		}
	}
	return after
}

// An inventory can aggregate several checkouts into one representative path.
// That does not prove that all those checkouts are inside a hidden tree.
func hiddenByProfile(p config.Profile, project protocol.InventoryProject) (config.ReviewRule, bool) {
	if project.CWDs > 1 {
		return config.ReviewRule{}, false
	}
	return p.Review.HiddenBy(config.ProjectID{CWD: project.CWD, CWDHash: project.CWDHash, GitRemote: project.GitRemote}, p.Projects.Allow)
}
