package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

// DefaultProfile is the profile a device gets when none was chosen for
// it.
const DefaultProfile = "default"

// ProfilesFileName is the lake's profiles file in its data directory.
const ProfilesFileName = "profiles.json"

// Profile is the base configuration a lake publishes to its agents. It
// holds a subset of config.json, and an agent's own config.json wins
// over every field of it.
//
// A profile may only narrow what a machine sends. It cannot set a
// harness root, which would point the agent at a directory of the
// lake's choosing, and it cannot set redaction.upload_hits, which would
// upload files the ruleset flagged. Both are refused when the profile
// loads on the lake and again when an agent reads it. projects.allow is
// the one field that widens, and it applies only to uploads to the lake
// that published it.
type Profile struct {
	Harnesses Harnesses       `json:"harnesses,omitempty"`
	Agent     AgentConfig     `json:"agent,omitzero"`
	Redaction RedactionConfig `json:"redaction,omitzero"`
	Projects  Projects        `json:"projects,omitzero"`
}

// Profiles is a lake's profiles by name. It always holds the default
// profile.
type Profiles map[string]Profile

type profilesFile struct {
	Profiles map[string]json.RawMessage `json:"profiles"`
}

// ValidProfileName reports whether name can name a profile. The rule is
// the one for lake names.
func ValidProfileName(name string) bool { return ValidLakeName(name) }

// ParseProfile decodes one profile strictly: a field outside the
// allowed set is an error, as is trailing data.
func ParseProfile(raw []byte) (Profile, error) {
	// Forbidden keys first, so the refusal names the rule rather than
	// what the harness decoder thinks of the value.
	if err := forbiddenKeys(raw); err != nil {
		return Profile{}, err
	}
	var p Profile
	if err := decodeStrict(raw, &p); err != nil {
		return Profile{}, err
	}
	if err := p.Validate(); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// forbiddenKeys refuses a profile that names a field it may not set,
// whatever the value. Validate sees only the decoded struct, where
// "upload_hits": false and "root": "" look like fields left out.
func forbiddenKeys(raw []byte) error {
	var keys struct {
		Harnesses map[string]map[string]json.RawMessage `json:"harnesses"`
		Redaction map[string]json.RawMessage            `json:"redaction"`
	}
	// A body this cannot read is left to decodeStrict to refuse.
	if json.Unmarshal(raw, &keys) != nil {
		return nil
	}
	names := make([]string, 0, len(keys.Harnesses))
	for id := range keys.Harnesses {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		if _, ok := keys.Harnesses[id]["root"]; ok {
			return fmt.Errorf("harnesses.%s.root: a profile cannot set a harness root; the machine's config.json sets it", id)
		}
	}
	if _, ok := keys.Redaction["upload_hits"]; ok {
		return errors.New("redaction.upload_hits: a profile cannot upload flagged files; the machine's config.json sets it")
	}
	return nil
}

// Validate refuses what a profile may not set and checks the debounce.
func (p Profile) Validate() error {
	names := make([]string, 0, len(p.Harnesses))
	for id := range p.Harnesses {
		names = append(names, id)
	}
	sort.Strings(names)
	for _, id := range names {
		if p.Harnesses[id].Root != "" {
			return fmt.Errorf("harnesses.%s.root: a profile cannot set a harness root; the machine's config.json sets it", id)
		}
	}
	if p.Redaction.UploadHits {
		return errors.New("redaction.upload_hits: a profile cannot upload flagged files; the machine's config.json sets it")
	}
	if _, _, err := p.Agent.Windows(); err != nil {
		return err
	}
	return nil
}

// Version names the profile's content: the same fields give the same
// version.
func (p Profile) Version() string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:8])
}

// LoadProfiles reads a profiles file:
//
//	{"profiles": {"default": {...}, "ci": {...}}}
//
// A missing file is a lake with one empty default profile. A file that
// names no default gets an empty one. Any unknown field, in the file or
// in a profile, fails the load.
func LoadProfiles(path string) (Profiles, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Profiles{DefaultProfile: {}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f profilesFile
	if err := decodeStrict(raw, &f); err != nil {
		return nil, fmt.Errorf("profiles: %s: %w", path, err)
	}
	out := Profiles{DefaultProfile: {}}
	for name, body := range f.Profiles {
		if !ValidProfileName(name) {
			return nil, fmt.Errorf("profiles: %s: %q: a profile name is lowercase letters, digits, '-' and '_', at most 32 characters", path, name)
		}
		p, err := ParseProfile(body)
		if err != nil {
			return nil, fmt.Errorf("profiles: %s: %s: %w", path, name, err)
		}
		out[name] = p
	}
	return out, nil
}

func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}
