// Package config is the per-user lampi directory: machine identity and
// the optional client config file.
//
// The architecture pins machine identity at ~/.config/terva-lampi/machine.json
// (XDG_CONFIG_HOME when that is set). The lake's own bytes live under the
// XDG state dir, separate from terva's $TERVA_HOME, which is a producer
// we read and do not own.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"terva.sh/lampi/internal/id"
)

// DefaultServer is the loopback lake a fresh client talks to.
const DefaultServer = "http://127.0.0.1:8787"

// File is the optional client config. Absent fields keep their defaults.
// The device token is never stored here; it lives in its own file.
//
// Projects is the gate for raw bytes leaving the machine. With no allow
// rule, sync refuses every project. Redaction.UploadHits is the only
// override that uploads a file ruleset v1 flagged.
//
// Harnesses is optional. Omit the map, or omit one harness id, and that
// harness keeps today's behavior. Unknown top-level keys are still
// ignored. A harness entry is not: it accepts enabled and root only.
type File struct {
	Server    string          `json:"server,omitempty"`
	TokenFile string          `json:"token_file,omitempty"`
	Projects  Projects        `json:"projects,omitempty"`
	Redaction RedactionConfig `json:"redaction,omitempty"`
	Harnesses Harnesses       `json:"harnesses,omitempty"`
}

// Machine is the stable identity written once.
type Machine struct {
	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname,omitempty"`
}

// ConfigDir is the directory that holds machine.json and, by default, the
// device token.
func ConfigDir(getenv func(string) string) (string, error) {
	if v := getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "terva-lampi"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if v := getenv("AppData"); v != "" {
			return filepath.Join(v, "terva-lampi"), nil
		}
	}
	return filepath.Join(home, ".config", "terva-lampi"), nil
}

// StateDir is the default lake data directory used by `terva-lampi serve`
// when --data is not set.
func StateDir(getenv func(string) string) (string, error) {
	if v := getenv("XDG_STATE_HOME"); v != "" {
		return filepath.Join(v, "terva-lampi"), nil
	}
	home, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" {
		if v := getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "terva-lampi"), nil
		}
	}
	return filepath.Join(home, ".local", "state", "terva-lampi"), nil
}

func homeDir(getenv func(string) string) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	if h := getenv("USERPROFILE"); h != "" {
		return h, nil
	}
	return "", fmt.Errorf("config: HOME is not set")
}

// MachinePath is machine.json inside the config dir.
func MachinePath(getenv func(string) string) (string, error) {
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "machine.json"), nil
}

// TokenPath is the default device-token file.
func TokenPath(getenv func(string) string) (string, error) {
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token"), nil
}

// ConfigPath is the optional config.json.
func ConfigPath(getenv func(string) string) (string, error) {
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadFile reads config.json. A missing file is an empty File, not an error.
func LoadFile(getenv func(string) string) (File, error) {
	path, err := ConfigPath(getenv)
	if err != nil {
		return File{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("config: %s: %w", path, err)
	}
	return f, nil
}

// LoadMachine reads machine.json. Missing is (zero, nil) so callers can
// decide whether to create one. A corrupt file is an error: silently
// minting a new id would split one machine's history.
func LoadMachine(getenv func(string) string) (Machine, error) {
	path, err := MachinePath(getenv)
	if err != nil {
		return Machine{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Machine{}, nil
	}
	if err != nil {
		return Machine{}, err
	}
	var m Machine
	if err := json.Unmarshal(b, &m); err != nil {
		return Machine{}, fmt.Errorf("config: %s: %w", path, err)
	}
	if m.MachineID == "" {
		return Machine{}, fmt.Errorf("config: %s has no machine_id", path)
	}
	return m, nil
}

// EnsureMachine returns the existing machine id, or writes a new one.
func EnsureMachine(getenv func(string) string) (Machine, error) {
	m, err := LoadMachine(getenv)
	if err != nil {
		return Machine{}, err
	}
	if m.MachineID != "" {
		return m, nil
	}
	uid, err := id.New(now())
	if err != nil {
		return Machine{}, err
	}
	host, _ := os.Hostname()
	m = Machine{MachineID: uid, Hostname: host}
	path, err := MachinePath(getenv)
	if err != nil {
		return Machine{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Machine{}, err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return Machine{}, err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return Machine{}, err
	}
	return m, nil
}

// ServerURL resolves the lake address: flag, then config file, then the
// loopback default. flagValue wins even when it equals the default, which
// is what a flag package already filled in.
func ServerURL(file File, flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if file.Server != "" {
		return file.Server
	}
	return DefaultServer
}
