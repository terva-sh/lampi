// Package regcode encodes and decodes registration codes.
//
// A code is what an operator hands to a new machine. It names the lake
// (its URL, lake id and the key that signed the code), carries a
// one-time secret and an expiry, and is signed by that key:
//
//	tlr1.<payload>.<signature>
//
// Both parts are unpadded base64url, so the code is URL-safe and one
// word. The payload is JSON. The signature is ed25519 over
// "terva-lampi/registration/v1", a zero byte, then the payload bytes.
//
// The code does not prove who the lake is: anyone can make a key and
// sign a code with it. register checks the key against the key list the
// URL serves over TLS, and shows the fingerprint for the person to
// compare with serve identity on the lake host.
package regcode

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/protocol"
)

// Prefix opens every code of this version.
const Prefix = "tlr1."

// Version is the payload's format version.
const Version = 1

// Code is a decoded registration code.
type Code struct {
	Version   int       `json:"v"`
	URL       string    `json:"url"`
	LakeID    string    `json:"lake_id"`
	KeyID     string    `json:"key_id"`
	PublicKey string    `json:"public_key"`
	Secret    string    `json:"secret"`
	Expires   time.Time `json:"expires"`
}

var b64 = base64.RawURLEncoding

// NewSecret is a fresh one-time secret: 32 random bytes, base64url.
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b64.EncodeToString(b), nil
}

// ValidSecret reports whether s has the shape NewSecret makes.
func ValidSecret(s string) bool {
	raw, err := b64.DecodeString(s)
	return err == nil && len(raw) == 32
}

// HashSecret is what the lake stores for a secret: hex SHA-256.
func HashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Encode signs c with id's first active key, filling in its lake id and
// key, and returns the code.
func Encode(id *identity.Identity, c Code, now time.Time) (string, error) {
	keys := id.ActiveKeys(now)
	if len(keys) == 0 {
		return "", errors.New("regcode: the lake has no active key")
	}
	pub := id.Public()
	c.Version = Version
	c.LakeID = id.LakeID
	c.KeyID = keys[0].ID
	for _, k := range pub {
		if k.ID == c.KeyID {
			c.PublicKey = k.PublicKey
		}
	}
	c.Expires = c.Expires.UTC()
	payload, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	k, sig, err := id.SignRaw(identity.ContextRegistration, payload, now)
	if err != nil {
		return "", err
	}
	if k.ID != c.KeyID {
		return "", errors.New("regcode: signing key changed")
	}
	return Prefix + b64.EncodeToString(payload) + "." + b64.EncodeToString(sig), nil
}

// Decode parses s and checks its signature against the key it names.
// It checks the shape of every field. It does not check the expiry,
// which the lake is the authority on, or that the key is the lake's.
func Decode(s string) (Code, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, Prefix) {
		if strings.HasPrefix(s, "tlr") {
			return Code{}, errors.New("regcode: this code is from a newer terva-lampi; upgrade to read it")
		}
		return Code{}, errors.New("regcode: not a registration code")
	}
	parts := strings.Split(strings.TrimPrefix(s, Prefix), ".")
	if len(parts) != 2 {
		return Code{}, errors.New("regcode: a code has two parts after the prefix")
	}
	payload, err := b64.DecodeString(parts[0])
	if err != nil {
		return Code{}, errors.New("regcode: payload does not decode")
	}
	sig, err := b64.DecodeString(parts[1])
	if err != nil {
		return Code{}, errors.New("regcode: signature does not decode")
	}
	var c Code
	if err := json.Unmarshal(payload, &c); err != nil {
		return Code{}, fmt.Errorf("regcode: payload: %w", err)
	}
	if c.Version != Version {
		return Code{}, fmt.Errorf("regcode: payload version %d", c.Version)
	}
	if !identity.ValidLakeID(c.LakeID) {
		return Code{}, errors.New("regcode: lake id is not valid")
	}
	if !ValidSecret(c.Secret) {
		return Code{}, errors.New("regcode: secret is not valid")
	}
	if c.URL == "" || c.Expires.IsZero() {
		return Code{}, errors.New("regcode: url and expiry are required")
	}
	pub, err := identity.ParsePublic(protocol.LakeKey{ID: c.KeyID, Alg: identity.AlgEd25519, PublicKey: c.PublicKey})
	if err != nil {
		return Code{}, fmt.Errorf("regcode: %w", err)
	}
	if !identity.VerifyRaw(identity.ContextRegistration, payload, sig, pub) {
		return Code{}, errors.New("regcode: signature does not verify")
	}
	return c, nil
}

// Fingerprint is the fingerprint of the key the code names, the one
// serve identity prints on the lake host.
func (c Code) Fingerprint() string {
	raw, err := b64.DecodeString(c.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return ""
	}
	return identity.Fingerprint(ed25519.PublicKey(raw))
}
