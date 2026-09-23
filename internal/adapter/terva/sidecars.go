package terva

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/discover"
)

// attachSidecars adds raati records and tasks archives to the session
// they belong to. A file with no session is left out of every manifest,
// so the allowlist never sees it on its own.
func attachSidecars(home string, order []string, groups map[string][]item, sidecars []item) error {
	for _, it := range sidecars {
		keys, err := sidecarKeys(home, it, order, groups)
		if err != nil {
			return err
		}
		for _, key := range keys {
			groups[key] = append(groups[key], it)
		}
	}
	return nil
}

func sidecarKeys(home string, it item, order []string, groups map[string][]item) ([]string, error) {
	switch it.file.Kind {
	case discover.KindTasks:
		return linkTasks(it, order, groups), nil
	case discover.KindRaati:
		return linkRaati(home, it, order, groups)
	default:
		return nil, nil
	}
}

func linkTasks(it item, order []string, groups map[string][]item) []string {
	id, ok := discover.TasksSessionID(it.file.RelPath)
	if !ok {
		return nil
	}
	if keys := groupsWithMetaID(order, groups, id); len(keys) > 0 {
		return keys
	}
	if !isHex16(id) {
		return nil
	}
	var keys []string
	for _, key := range order {
		for _, g := range groups[key] {
			if g.file.Kind == discover.KindTranscript && g.meta.id != "" && tasksHash(g.meta.id) == id {
				keys = append(keys, key)
				break
			}
		}
	}
	return keys
}

func linkRaati(home string, it item, order []string, groups map[string][]item) ([]string, error) {
	raw, err := os.ReadFile(it.file.AbsPath)
	if err != nil {
		return nil, err
	}
	var rec struct {
		Units []struct {
			AgentID      string `json:"agent_id"`
			BlindAgentID string `json:"blind_agent_id"`
		} `json:"units"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		// A partial write is not a record yet. The next sync reads it.
		return nil, nil
	}
	seenSession := map[string]struct{}{}
	seenOrigin := map[string]struct{}{}
	var sessionIDs []string
	var origins []string
	for _, u := range rec.Units {
		for _, id := range []string{u.AgentID, u.BlindAgentID} {
			meta, ok := readSwarmMeta(home, id)
			if !ok {
				continue
			}
			if meta.SessionID != "" {
				if _, ok := seenSession[meta.SessionID]; !ok {
					seenSession[meta.SessionID] = struct{}{}
					sessionIDs = append(sessionIDs, meta.SessionID)
				}
			}
			origin := meta.Origin
			if origin == "" {
				origin = meta.Dir
			}
			if origin == "" {
				continue
			}
			if _, ok := seenOrigin[origin]; ok {
				continue
			}
			seenOrigin[origin] = struct{}{}
			origins = append(origins, origin)
		}
	}
	seen := map[string]struct{}{}
	var keys []string
	add := func(more []string) {
		for _, key := range more {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	for _, id := range sessionIDs {
		add(groupsWithMetaID(order, groups, id))
	}
	if len(keys) > 0 {
		return keys, nil
	}
	for _, origin := range origins {
		add(groupsWithCWD(order, groups, origin))
	}
	return keys, nil
}

type swarmMeta struct {
	SessionID string `json:"session_id"`
	Origin    string `json:"origin"`
	Dir       string `json:"dir"`
}

// readSwarmMeta reads the spawn record for a raati seat. Live agents
// win over the archive. An id that could leave the swarm tree is ignored.
func readSwarmMeta(home, id string) (swarmMeta, bool) {
	if !safeAgentID(id) {
		return swarmMeta{}, false
	}
	for _, dir := range []string{"agents", "archive"} {
		path := filepath.Join(home, "swarm", dir, id, "meta.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var meta swarmMeta
		if json.Unmarshal(raw, &meta) != nil {
			continue
		}
		return meta, true
	}
	return swarmMeta{}, false
}

func safeAgentID(id string) bool {
	if id == "" || id == "." || id == ".." || strings.Contains(id, "..") {
		return false
	}
	return !strings.ContainsAny(id, `/\`)
}

func groupsWithMetaID(order []string, groups map[string][]item, id string) []string {
	var keys []string
	for _, key := range order {
		for _, it := range groups[key] {
			if it.file.Kind == discover.KindTranscript && it.meta.id == id {
				keys = append(keys, key)
				break
			}
		}
	}
	return keys
}

func groupsWithCWD(order []string, groups map[string][]item, cwd string) []string {
	cwd = filepath.Clean(cwd)
	if cwd == "" || cwd == "." {
		return nil
	}
	var keys []string
	for _, key := range order {
		for _, it := range groups[key] {
			if it.file.Kind != discover.KindTranscript || !it.meta.ok || it.meta.cwd == "" {
				continue
			}
			if filepath.Clean(it.meta.cwd) == cwd {
				keys = append(keys, key)
				break
			}
		}
	}
	return keys
}

func tasksHash(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:8])
}

func isHex16(s string) bool {
	if len(s) != 16 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
