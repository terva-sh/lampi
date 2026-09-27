package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	"terva.sh/lampi/internal/protocol"
)

// endorsement is what a key signs to vouch for the next one.
type endorsement struct {
	LakeID    string `json:"lake_id"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
}

func endorsementPayload(lakeID string, keyID string, pub ed25519.PublicKey) []byte {
	raw, _ := json.Marshal(endorsement{LakeID: lakeID, KeyID: keyID, PublicKey: base64.RawURLEncoding.EncodeToString(pub)})
	return raw
}

// Rotate adds a new active key, endorsed by the current key, and ends
// every other active key's window at now+overlap. During the overlap
// both keys sign, so an agent that has not moved its pin yet still
// verifies the lake, and one that has verifies it too.
func (id *Identity) Rotate(r io.Reader, now time.Time, overlap time.Duration) (Key, error) {
	cur, ok := id.Current(now)
	if !ok {
		return Key{}, errors.New("identity: no active key to endorse a new one; a lake with none must be registered again by every agent")
	}
	if cur.Compromised {
		return Key{}, errors.New("identity: the current key is compromised")
	}
	pub, priv, err := ed25519.GenerateKey(r)
	if err != nil {
		return Key{}, fmt.Errorf("identity: %w", err)
	}
	k := Key{
		ID:         KeyID(pub),
		Status:     StatusActive,
		Created:    now.UTC().Truncate(time.Second),
		Pub:        pub,
		priv:       priv,
		EndorsedBy: cur.ID,
	}
	// A key made within the same second as the current one would not
	// sort after it.
	if !k.Created.After(cur.Created) {
		k.Created = cur.Created.Add(time.Second)
	}
	k.Endorsement = ed25519.Sign(cur.priv, message(ContextEndorse, endorsementPayload(id.LakeID, k.ID, pub)))
	end := now.Add(overlap).UTC().Truncate(time.Second)
	for i := range id.Keys {
		old := &id.Keys[i]
		if old.Active(now) && (old.NotAfter.IsZero() || old.NotAfter.After(end)) {
			old.NotAfter = end
		}
	}
	id.Keys = append(id.Keys, k)
	return k, nil
}

// Retire ends a key's window now. compromised marks it as possibly
// leaked, so nothing it endorsed is followed. The last active key
// cannot be retired: rotate first, so there is a key to sign with.
func (id *Identity) Retire(keyID string, now time.Time, compromised bool) error {
	idx := -1
	active := 0
	for i, k := range id.Keys {
		if k.ID == keyID {
			idx = i
		}
		if k.Active(now) {
			active++
		}
	}
	if idx < 0 {
		return fmt.Errorf("identity: no key %s", keyID)
	}
	k := &id.Keys[idx]
	if k.Active(now) && active == 1 {
		return fmt.Errorf("identity: key %s is the only active key; run serve identity rotate first", keyID)
	}
	k.Status = StatusRetired
	if k.NotAfter.IsZero() || k.NotAfter.After(now) {
		k.NotAfter = now.UTC().Truncate(time.Second)
	}
	if compromised {
		k.Compromised = true
	}
	return nil
}

// VerifyKeyList checks that a published key list is signed by the
// pinned key or by a key that chains to it through endorsements. The
// chain here ignores compromised marks, which the list itself carries
// and so cannot be trusted before its signature is.
func VerifyKeyList(lakeID string, pinned protocol.LakeKey, s *protocol.Signed, keys []protocol.LakeKey) error {
	trusted := map[string]protocol.LakeKey{pinned.ID: pinned}
	for grew := true; grew; {
		grew = false
		for _, k := range keys {
			if _, ok := trusted[k.ID]; ok {
				continue
			}
			if by, ok := trusted[k.EndorsedBy]; ok && endorsed(lakeID, by, k) {
				trusted[k.ID] = k
				grew = true
			}
		}
	}
	for _, k := range trusted {
		pub, err := ParsePublic(k)
		if err != nil {
			continue
		}
		if Verify(ContextKeys, s, pub) == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: no key it chains to signed the list", ErrNoChain)
}

// Chain errors. Each means the agent cannot trust the list from its pin.
var (
	ErrPinCompromised = errors.New("the lake marks the pinned key compromised")
	ErrNoChain        = errors.New("the key list has no chain from the pinned key")
)

// Advance finds where a client pinned to pinned should move its pin in
// a published key list. It follows endorsements from the pin, skipping
// any key the list marks compromised, and returns the newest active key
// it reaches, which may be the pin itself. keys must already be
// verified as signed by a key the caller trusts.
func Advance(lakeID string, pinned protocol.LakeKey, keys []protocol.LakeKey, now time.Time) (protocol.LakeKey, error) {
	byID := map[string]protocol.LakeKey{}
	for _, k := range keys {
		byID[k.ID] = k
	}
	if k, ok := byID[pinned.ID]; ok {
		if k.PublicKey != pinned.PublicKey {
			return protocol.LakeKey{}, fmt.Errorf("%w: key %s has another public key there", ErrNoChain, pinned.ID)
		}
		if k.Compromised {
			return protocol.LakeKey{}, ErrPinCompromised
		}
	}
	trusted := map[string]protocol.LakeKey{pinned.ID: pinned}
	for grew := true; grew; {
		grew = false
		for _, k := range keys {
			if _, ok := trusted[k.ID]; ok || k.Compromised {
				continue
			}
			by, ok := trusted[k.EndorsedBy]
			if !ok || !endorsed(lakeID, by, k) {
				continue
			}
			trusted[k.ID] = k
			grew = true
		}
	}
	var candidates []protocol.LakeKey
	for id := range trusted {
		k, listed := byID[id]
		if listed && keyActive(k, now) {
			candidates = append(candidates, k)
		}
	}
	if len(candidates) == 0 {
		return protocol.LakeKey{}, ErrNoChain
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Created.After(candidates[j].Created) })
	return candidates[0], nil
}

func endorsed(lakeID string, by, k protocol.LakeKey) bool {
	pub, err := ParsePublic(by)
	if err != nil {
		return false
	}
	kpub, err := ParsePublic(k)
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(k.Endorsement)
	if err != nil {
		return false
	}
	return VerifyRaw(ContextEndorse, endorsementPayload(lakeID, k.ID, kpub), sig, pub)
}

func keyActive(k protocol.LakeKey, now time.Time) bool {
	if k.Status != StatusActive || k.Compromised {
		return false
	}
	return k.NotAfter == nil || now.Before(*k.NotAfter)
}
