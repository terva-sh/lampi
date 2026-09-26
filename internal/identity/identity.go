// Package identity is the lake's signing identity: a random lake id and
// a list of ed25519 keys, kept in identity.json in the lake directory at
// mode 0600.
//
// Agents pin the lake id and a key when they register. A lake that lost
// identity.json and made a new one would look like a different lake to
// every agent, so the catalog records the lake id the first time an
// identity is made, and Ensure refuses to make another over it.
//
// The same package verifies what a lake signed. A signature covers a
// context string and the exact payload bytes, so a signature made for
// the key list does not verify as a hello proof.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// FileName is the identity file in the lake directory.
const FileName = "identity.json"

// fileVersion is the identity.json format this binary writes and reads.
const fileVersion = 1

// Key statuses.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// AlgEd25519 is the only signature algorithm.
const AlgEd25519 = "ed25519"

// Signing contexts. Each is signed as "terva-lampi/<context>\x00" then
// the payload, so a signature cannot be moved from one use to another.
const (
	ContextKeys  = "keys/v1"
	ContextHello = "hello/v1"
)

// Key is one signing key. Priv is nil for a key parsed from a published
// list.
type Key struct {
	ID       string
	Status   string
	Created  time.Time
	NotAfter time.Time
	Pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
}

// Active reports whether k may sign at now.
func (k Key) Active(now time.Time) bool {
	if k.Status != StatusActive {
		return false
	}
	return k.NotAfter.IsZero() || now.Before(k.NotAfter)
}

// Identity is a lake id and its keys.
type Identity struct {
	LakeID string
	Keys   []Key
}

type fileKey struct {
	ID       string     `json:"id"`
	Alg      string     `json:"alg"`
	Seed     string     `json:"seed"`
	Status   string     `json:"status"`
	Created  time.Time  `json:"created"`
	NotAfter *time.Time `json:"not_after,omitempty"`
}

type file struct {
	Version int       `json:"version"`
	LakeID  string    `json:"lake_id"`
	Keys    []fileKey `json:"keys"`
}

// Path is identity.json under the lake directory.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// KeyID is the first 8 bytes of the key's SHA-256, in hex.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// Fingerprint is the key's SHA-256 in the form ssh prints a host key:
// "SHA256:" and unpadded standard base64. An operator compares it by eye.
func Fingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// Load reads identity.json from dir. A missing file is an error that
// satisfies errors.Is(err, os.ErrNotExist).
func Load(dir string) (*Identity, error) {
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil, err
	}
	var f file
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("identity: %s: %w", Path(dir), err)
	}
	if f.Version != fileVersion {
		return nil, fmt.Errorf("identity: %s: version %d; this binary reads %d", Path(dir), f.Version, fileVersion)
	}
	if !validLakeID(f.LakeID) {
		return nil, fmt.Errorf("identity: %s: lake_id %q is not a lake id", Path(dir), f.LakeID)
	}
	if len(f.Keys) == 0 {
		return nil, fmt.Errorf("identity: %s: no keys", Path(dir))
	}
	id := &Identity{LakeID: f.LakeID}
	seen := map[string]bool{}
	for i, fk := range f.Keys {
		if fk.Alg != AlgEd25519 {
			return nil, fmt.Errorf("identity: %s: key %d: alg %q", Path(dir), i+1, fk.Alg)
		}
		if fk.Status != StatusActive && fk.Status != StatusRetired {
			return nil, fmt.Errorf("identity: %s: key %d: status %q", Path(dir), i+1, fk.Status)
		}
		seed, err := base64.RawURLEncoding.DecodeString(fk.Seed)
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, fmt.Errorf("identity: %s: key %d: seed does not decode", Path(dir), i+1)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		pub := priv.Public().(ed25519.PublicKey)
		if KeyID(pub) != fk.ID {
			return nil, fmt.Errorf("identity: %s: key %d: id %q does not match its key", Path(dir), i+1, fk.ID)
		}
		if seen[fk.ID] {
			return nil, fmt.Errorf("identity: %s: key %s listed twice", Path(dir), fk.ID)
		}
		seen[fk.ID] = true
		k := Key{ID: fk.ID, Status: fk.Status, Created: fk.Created, Pub: pub, priv: priv}
		if fk.NotAfter != nil {
			k.NotAfter = *fk.NotAfter
		}
		id.Keys = append(id.Keys, k)
	}
	return id, nil
}

// New makes an identity with one active key, from rand.
func New(r io.Reader, now time.Time) (*Identity, error) {
	idb := make([]byte, 16)
	if _, err := io.ReadFull(r, idb); err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	pub, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	return &Identity{
		LakeID: lakeIDPrefix + strings.ToLower(lakeIDEncoding.EncodeToString(idb)),
		Keys: []Key{{
			ID:      KeyID(pub),
			Status:  StatusActive,
			Created: now.UTC().Truncate(time.Second),
			Pub:     pub,
			priv:    priv,
		}},
	}, nil
}

const lakeIDPrefix = "lake_"

var lakeIDEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// validLakeID accepts what New makes: the prefix and 26 lowercase
// base32 characters.
func validLakeID(s string) bool {
	rest, ok := strings.CutPrefix(s, lakeIDPrefix)
	if !ok || len(rest) != 26 {
		return false
	}
	for _, c := range rest {
		if !(c >= 'a' && c <= 'z' || c >= '2' && c <= '7') {
			return false
		}
	}
	return true
}

// ValidLakeID reports whether s has the shape of a lake id.
func ValidLakeID(s string) bool { return validLakeID(s) }

// create writes id to dir as a new identity.json. It refuses when the
// file already exists, so two processes that race to make an identity
// cannot both win: the loser's link fails.
func create(dir string, id *Identity) error {
	f := file{Version: fileVersion, LakeID: id.LakeID}
	for _, k := range id.Keys {
		fk := fileKey{
			ID:      k.ID,
			Alg:     AlgEd25519,
			Seed:    base64.RawURLEncoding.EncodeToString(k.priv.Seed()),
			Status:  k.Status,
			Created: k.Created,
		}
		if !k.NotAfter.IsZero() {
			t := k.NotAfter
			fk.NotAfter = &t
		}
		f.Keys = append(f.Keys, fk)
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".identity-*")
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("identity: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("identity: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("identity: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	if err := os.Link(name, Path(dir)); err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("identity: %w", err)
	}
	defer d.Close()
	// Some platforms cannot fsync a directory. The link is still made.
	_ = d.Sync()
	return nil
}

// Recorder is where the lake id is recorded once an identity exists.
// The catalog implements it.
type Recorder interface {
	LakeID() (string, error)
	RecordLakeID(id string) error
}

// Ensure loads dir's identity, or makes one when the lake has never had
// one, and checks it against the lake id rec holds.
//
//   - identity.json and a recorded id that differ: refused.
//   - identity.json and no recorded id: the id is recorded.
//   - no identity.json and a recorded id: refused, because agents have
//     pinned a key this lake no longer holds. Restore identity.json.
//   - neither: a new identity is made and recorded. This is a new lake,
//     or one upgrading from a release that had no identity.
//
// created is true when this call made the identity.
func Ensure(dir string, rec Recorder, r io.Reader, now time.Time) (id *Identity, created bool, err error) {
	recorded, err := rec.LakeID()
	if err != nil {
		return nil, false, err
	}
	id, err = Load(dir)
	switch {
	case err == nil:
		if recorded != "" && recorded != id.LakeID {
			return nil, false, fmt.Errorf("identity: %s holds lake %s but the catalog is lake %s; restore the matching identity.json from a backup", Path(dir), id.LakeID, recorded)
		}
		if recorded == "" {
			if err := rec.RecordLakeID(id.LakeID); err != nil {
				return nil, false, err
			}
		}
		return id, false, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, false, err
	case recorded != "":
		return nil, false, fmt.Errorf("identity: %s is missing but the catalog is lake %s; agents pin its key, so restore identity.json from a backup instead of making a new one", Path(dir), recorded)
	}
	id, err = New(r, now)
	if err != nil {
		return nil, false, err
	}
	if err := create(dir, id); err != nil {
		return nil, false, err
	}
	if err := rec.RecordLakeID(id.LakeID); err != nil {
		return nil, false, err
	}
	return id, true, nil
}

// ActiveKeys are the keys that sign at now.
func (id *Identity) ActiveKeys(now time.Time) []Key {
	var out []Key
	for _, k := range id.Keys {
		if k.Active(now) && k.priv != nil {
			out = append(out, k)
		}
	}
	return out
}

// Public is the published key list.
func (id *Identity) Public() []protocol.LakeKey {
	out := make([]protocol.LakeKey, 0, len(id.Keys))
	for _, k := range id.Keys {
		lk := protocol.LakeKey{
			ID:        k.ID,
			Alg:       AlgEd25519,
			PublicKey: base64.RawURLEncoding.EncodeToString(k.Pub),
			Status:    k.Status,
			Created:   k.Created,
		}
		if !k.NotAfter.IsZero() {
			t := k.NotAfter
			lk.NotAfter = &t
		}
		out = append(out, lk)
	}
	return out
}

func message(context string, payload []byte) []byte {
	m := make([]byte, 0, len("terva-lampi/")+len(context)+1+len(payload))
	m = append(m, "terva-lampi/"...)
	m = append(m, context...)
	m = append(m, 0)
	return append(m, payload...)
}

// Sign encodes payload and signs it with every key active at now.
func (id *Identity) Sign(context string, payload any, now time.Time) (*protocol.Signed, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	keys := id.ActiveKeys(now)
	if len(keys) == 0 {
		return nil, errors.New("identity: no active key")
	}
	s := &protocol.Signed{Payload: raw}
	msg := message(context, raw)
	for _, k := range keys {
		s.Signatures = append(s.Signatures, protocol.Signature{
			KeyID: k.ID,
			Alg:   AlgEd25519,
			Sig:   base64.RawURLEncoding.EncodeToString(ed25519.Sign(k.priv, msg)),
		})
	}
	return s, nil
}

// ParsePublic decodes a published key.
func ParsePublic(k protocol.LakeKey) (ed25519.PublicKey, error) {
	if k.Alg != AlgEd25519 {
		return nil, fmt.Errorf("identity: key %s: alg %q", k.ID, k.Alg)
	}
	raw, err := base64.RawURLEncoding.DecodeString(k.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("identity: key %s: public key does not decode", k.ID)
	}
	pub := ed25519.PublicKey(raw)
	if KeyID(pub) != k.ID {
		return nil, fmt.Errorf("identity: key %s: id does not match its key", k.ID)
	}
	return pub, nil
}

// Verify checks that s carries a valid signature by pub for context.
// It checks the payload bytes as received.
func Verify(context string, s *protocol.Signed, pub ed25519.PublicKey) error {
	if s == nil {
		return errors.New("identity: nothing signed")
	}
	id := KeyID(pub)
	msg := message(context, s.Payload)
	for _, sig := range s.Signatures {
		if sig.KeyID != id {
			continue
		}
		if sig.Alg != AlgEd25519 {
			return fmt.Errorf("identity: key %s: alg %q", id, sig.Alg)
		}
		raw, err := base64.RawURLEncoding.DecodeString(sig.Sig)
		if err != nil {
			return fmt.Errorf("identity: key %s: signature does not decode", id)
		}
		if !ed25519.Verify(pub, msg, raw) {
			return fmt.Errorf("identity: key %s: signature does not verify", id)
		}
		return nil
	}
	return fmt.Errorf("identity: no signature by key %s", id)
}

// MaxNonce is the longest nonce a lake signs back.
const MaxNonce = 128

// ValidNonce accepts an empty nonce, or up to MaxNonce characters of
// unpadded base64url or hex.
func ValidNonce(s string) bool {
	if len(s) > MaxNonce {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// NewNonce is 32 random bytes as unpadded base64url.
func NewNonce() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
