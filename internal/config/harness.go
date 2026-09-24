package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"

	"terva.sh/lampi/internal/protocol"
)

// Harnesses is the optional Shape A map on a client config. Keys are
// protocol harness ids. A nil map, an empty map, and a missing key are
// the same: that harness keeps today's behavior.
type Harnesses map[string]HarnessConfig

// HarnessConfig is one harness entry. After a load, Enabled is true
// unless the file set false. Root is empty unless the file set an
// absolute path. The path is stored as written and does not have to
// exist.
//
// Root precedence, for a later caller, is flag (if one exists), then
// this root, then the harness env var, then the adapter default. No
// flag exists today. This package does not read the env var, does not
// add a flag, and does not change discover or watch.
type HarnessConfig struct {
	Enabled bool   `json:"enabled"`
	Root    string `json:"root,omitempty"`
}

// Enabled reports whether id is on. No map, or no entry for id, is on.
// That is today's behavior. An entry is on unless the file set false.
func (h Harnesses) Enabled(id string) bool {
	e, ok := h[id]
	if !ok {
		return true
	}
	return e.Enabled
}

// UnmarshalJSON rejects an unknown harness id. The id is named in the
// error. Null is the same as omitting the map.
func (h *Harnesses) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		*h = nil
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if raw == nil {
		*h = nil
		return nil
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(Harnesses, len(raw))
	for _, key := range keys {
		if !knownHarness(key) {
			return fmt.Errorf("unknown harness %q", key)
		}
		var entry HarnessConfig
		if err := json.Unmarshal(raw[key], &entry); err != nil {
			return fmt.Errorf("harness %q: %w", key, err)
		}
		out[key] = entry
	}
	*h = out
	return nil
}

// UnmarshalJSON accepts enabled and root only. Unknown fields, including
// allowlist and secret fields, are a load error. enabled omitted is
// true. root omitted is empty. An empty or relative root is a load error.
func (h *HarnessConfig) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || b[0] != '{' {
		return fmt.Errorf("harness entry must be an object")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var raw struct {
		Enabled json.RawMessage `json:"enabled"`
		Root    json.RawMessage `json:"root"`
	}
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	h.Enabled = true
	if raw.Enabled != nil {
		var on bool
		if err := json.Unmarshal(raw.Enabled, &on); err != nil {
			return fmt.Errorf("enabled: %w", err)
		}
		h.Enabled = on
	}
	if raw.Root == nil {
		return nil
	}
	var root string
	if err := json.Unmarshal(raw.Root, &root); err != nil {
		return fmt.Errorf("root: %w", err)
	}
	if root == "" {
		return fmt.Errorf("root is empty")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("root %q is not an absolute path", root)
	}
	h.Root = root
	return nil
}

func knownHarness(id string) bool {
	switch id {
	case protocol.HarnessTerva,
		protocol.HarnessClaude,
		protocol.HarnessCodex,
		protocol.HarnessOpenCode,
		protocol.HarnessCursor,
		protocol.HarnessCursorCLI:
		return true
	default:
		return false
	}
}
