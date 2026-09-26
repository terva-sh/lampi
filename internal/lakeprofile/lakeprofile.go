// Package lakeprofile verifies and caches the base configuration a lake
// signs for its agents.
//
// A profile is accepted only from a lake the agent pinned at
// registration: the lake id and one key. The signature is checked
// against that key, the payload's lake id against the pinned one, and
// the profile is decoded strictly, so a field a lake may not set is
// refused here even if the lake would publish it. The last copy that
// passed is kept in the lake's state directory and used when a fetch
// fails or returns a copy that does not pass.
package lakeprofile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

// FileName is the cached signed profile in a lake's state directory.
const FileName = "profile.json"

// Doc is a verified profile: the signed envelope as received, its
// payload, and the profile it carries.
type Doc struct {
	Signed  *protocol.Signed
	Payload protocol.AgentConfigPayload
	Profile config.Profile
}

// ErrNotPinned is Verify on a lake with no pinned identity.
var ErrNotPinned = errors.New("lake has no pinned key; register it to accept its profile")

// Pinned reports whether the lake has the identity a profile is checked
// against.
func Pinned(l config.Lake) bool {
	return l.LakeID != "" && l.KeyID != "" && l.PublicKey != ""
}

// Verify checks s against the lake's pin and decodes the profile.
func Verify(s *protocol.Signed, l config.Lake) (Doc, error) {
	if !Pinned(l) {
		return Doc{}, ErrNotPinned
	}
	pub, err := identity.ParsePublic(protocol.LakeKey{ID: l.KeyID, Alg: identity.AlgEd25519, PublicKey: l.PublicKey})
	if err != nil {
		return Doc{}, fmt.Errorf("pinned key: %w", err)
	}
	if err := identity.Verify(identity.ContextAgentConfig, s, pub); err != nil {
		return Doc{}, err
	}
	var p protocol.AgentConfigPayload
	if err := json.Unmarshal(s.Payload, &p); err != nil {
		return Doc{}, fmt.Errorf("profile payload: %w", err)
	}
	if p.LakeID != l.LakeID {
		return Doc{}, fmt.Errorf("profile is for lake %s, pinned lake is %s", p.LakeID, l.LakeID)
	}
	prof, err := config.ParseProfile(p.Config)
	if err != nil {
		return Doc{}, fmt.Errorf("profile %s: %w", p.Profile, err)
	}
	return Doc{Signed: s, Payload: p, Profile: prof}, nil
}

// Load reads and verifies the cached profile in dir. A missing file is
// (Doc{}, false, nil). A file that no longer verifies, because the pin
// changed or the file was edited, is an error and is not used.
func Load(dir string, l config.Lake) (Doc, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return Doc{}, false, nil
	}
	if err != nil {
		return Doc{}, false, err
	}
	var s protocol.Signed
	if err := json.Unmarshal(raw, &s); err != nil {
		return Doc{}, false, fmt.Errorf("cached profile: %w", err)
	}
	d, err := Verify(&s, l)
	if err != nil {
		return Doc{}, false, fmt.Errorf("cached profile: %w", err)
	}
	return d, true, nil
}

// Save writes a verified document to dir, replacing the cached copy.
func Save(dir string, d Doc) error {
	raw, err := json.Marshal(d.Signed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return config.WriteFileAtomic(filepath.Join(dir, FileName), raw)
}
