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
// override that uploads a file ruleset v2 flagged.
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
	if err := writeFileAtomic(path, raw); err != nil {
		return Machine{}, err
	}
	return m, nil
}

// writeFileAtomic writes a complete owner-only file beside path and
// renames it over path. A crash leaves the old file or none, never a
// torn one that LoadMachine would reject.
func writeFileAtomic(path string, raw []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		tmp.Close()
		if name != "" {
			os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	name = ""
	return nil
}

// Setting is one resolved value and the layer that won it: flag, env,
// config, or default. Every command that talks to a lake resolves the
// server and the token file through the same two functions, so status
// reports the address the agent uses.
type Setting struct {
	Value  string
	Source string
}

// Setting sources, in the order they are tried.
const (
	SourceFlag    = "flag"
	SourceEnv     = "env"
	SourceConfig  = "config"
	SourceDefault = "default"
)

// ResolveServer picks the lake URL: the flag, then LAMPI_SERVER, then
// server in config.json, then DefaultServer.
func ResolveServer(file File, getenv func(string) string, flagValue string) Setting {
	return pick(flagValue, getenv("LAMPI_SERVER"), file.Server, DefaultServer)
}

// ResolveTokenFile picks the device token file: the flag, then
// LAMPI_TOKEN_FILE, then token_file in config.json, then TokenPath.
func ResolveTokenFile(file File, getenv func(string) string, flagValue string) (Setting, error) {
	def := ""
	if flagValue == "" && getenv("LAMPI_TOKEN_FILE") == "" && file.TokenFile == "" {
		p, err := TokenPath(getenv)
		if err != nil {
			return Setting{}, err
		}
		def = p
	}
	return pick(flagValue, getenv("LAMPI_TOKEN_FILE"), file.TokenFile, def), nil
}

func pick(flagValue, envValue, fileValue, def string) Setting {
	switch {
	case flagValue != "":
		return Setting{Value: flagValue, Source: SourceFlag}
	case envValue != "":
		return Setting{Value: envValue, Source: SourceEnv}
	case fileValue != "":
		return Setting{Value: fileValue, Source: SourceConfig}
	default:
		return Setting{Value: def, Source: SourceDefault}
	}
}
