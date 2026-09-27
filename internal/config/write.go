package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LakeTokenPath is the default token file of the lake called name: the
// legacy token for default, tokens/<name>.token for the rest.
func LakeTokenPath(getenv func(string) string, name string) (string, error) {
	return lakeTokenPath(getenv, name)
}

// SetLake writes lakes.<name> into config.json, creating the file and
// its directory when they do not exist. Every other key is kept as it
// was, including keys this release does not know.
func SetLake(getenv func(string) string, name string, lc LakeConfig) error {
	return Locked(getenv, func(tx Tx) error { return tx.SetLake(name, lc) })
}

// SetLake is SetLake under a lock the caller holds.
func (tx Tx) SetLake(name string, lc LakeConfig) error {
	getenv := tx.getenv
	if !ValidLakeName(name) {
		return fmt.Errorf("config: %q is not a lake name", name)
	}
	if err := checkLakeEntry(name, lc); err != nil {
		return err
	}
	return editConfig(getenv, func(top map[string]json.RawMessage, lakes map[string]json.RawMessage) error {
		raw, err := json.Marshal(lc)
		if err != nil {
			return err
		}
		lakes[name] = raw
		return nil
	})
}

// UpdateLake rewrites lakes.<name> in config.json with what edit makes
// of the entry as it is read for this write, so a check edit makes
// holds for the entry it replaces. An error from edit writes nothing.
func UpdateLake(getenv func(string) string, name string, edit func(*LakeConfig) error) error {
	return Locked(getenv, func(tx Tx) error { return tx.UpdateLake(name, edit) })
}

// UpdateLake is UpdateLake under a lock the caller holds.
func (tx Tx) UpdateLake(name string, edit func(*LakeConfig) error) error {
	return editConfig(tx.getenv, func(top map[string]json.RawMessage, lakes map[string]json.RawMessage) error {
		raw, ok := lakes[name]
		if !ok {
			return fmt.Errorf("config: no lake named %s in the lakes map", name)
		}
		var lc LakeConfig
		if err := json.Unmarshal(raw, &lc); err != nil {
			return fmt.Errorf("config: lakes.%s: %w", name, err)
		}
		if err := edit(&lc); err != nil {
			return err
		}
		if err := checkLakeEntry(name, lc); err != nil {
			return err
		}
		out, err := json.Marshal(lc)
		if err != nil {
			return err
		}
		lakes[name] = out
		return nil
	})
}

// RemoveLake deletes lakes.<name> from config.json. Removing the last
// lake leaves an empty lakes map, which is the standalone state, not
// the loopback default a missing map means.
func RemoveLake(getenv func(string) string, name string) error {
	return Locked(getenv, func(tx Tx) error {
		return editConfig(tx.getenv, func(top map[string]json.RawMessage, lakes map[string]json.RawMessage) error {
			if _, ok := lakes[name]; !ok {
				return fmt.Errorf("config: no lake named %s in the lakes map", name)
			}
			delete(lakes, name)
			return nil
		})
	})
}

// Lake reads lakes.<name> as config.json holds it now. ok is false when
// there is no such entry.
func (tx Tx) Lake(name string) (lc LakeConfig, ok bool, err error) {
	path, err := ConfigPath(tx.getenv)
	if err != nil {
		return LakeConfig{}, false, err
	}
	_, lakes, err := readConfig(path)
	if err != nil {
		return LakeConfig{}, false, err
	}
	raw, ok := lakes[name]
	if !ok {
		return LakeConfig{}, false, nil
	}
	if err := json.Unmarshal(raw, &lc); err != nil {
		return LakeConfig{}, false, fmt.Errorf("config: lakes.%s: %w", name, err)
	}
	return lc, true, nil
}

// editConfig reads config.json as raw keys, lets edit change the lakes
// map, and writes the file back atomically at 0600. The lakes map is
// always written, as {} when it is empty. The caller holds the lock.
func editConfig(getenv func(string) string, edit func(top, lakes map[string]json.RawMessage) error) error {
	path, err := ConfigPath(getenv)
	if err != nil {
		return err
	}
	top, lakes, err := readConfig(path)
	if err != nil {
		return err
	}
	if err := edit(top, lakes); err != nil {
		return err
	}
	encoded, err := json.Marshal(lakes)
	if err != nil {
		return err
	}
	top["lakes"] = encoded
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileAtomic(path, append(out, '\n'))
}

// readConfig reads config.json as raw top-level keys and its lakes map.
// A missing or empty file is two empty maps.
func readConfig(path string) (top, lakes map[string]json.RawMessage, err error) {
	top = map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, nil, err
	case len(bytes.TrimSpace(raw)) > 0:
		if err := json.Unmarshal(raw, &top); err != nil {
			return nil, nil, fmt.Errorf("config: %s: %w", path, err)
		}
		// A file holding null unmarshals to a nil map; LoadFile reads it
		// as an empty config, and so does a write.
		if top == nil {
			top = map[string]json.RawMessage{}
		}
	}
	lakes = map[string]json.RawMessage{}
	if l, ok := top["lakes"]; ok && string(bytes.TrimSpace(l)) != "null" {
		if err := json.Unmarshal(l, &lakes); err != nil {
			return nil, nil, fmt.Errorf("config: %s: lakes: %w", path, err)
		}
	}
	// A null lakes map reads as empty; the write puts {} back.
	if lakes == nil {
		lakes = map[string]json.RawMessage{}
	}
	return top, lakes, nil
}
