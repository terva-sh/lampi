package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

// pinRefused is a key list that cannot be trusted from the pin: the
// pinned key is compromised, or nothing chains to it. It is not a
// network failure, and it does not pass with time.
type pinRefused struct{ err error }

func (e *pinRefused) Error() string {
	if errors.Is(e.err, identity.ErrPinCompromised) {
		return e.err.Error() + "; this machine must register with the lake again: ask for a new code and run terva-lampi register --replace"
	}
	return e.err.Error() + "; if the lake was rebuilt or its keys were replaced, register again with terva-lampi register --replace"
}

func (e *pinRefused) Unwrap() error { return e.err }

// pinOf is the lake's pinned key as a published key.
func pinOf(l config.Lake) protocol.LakeKey {
	return protocol.LakeKey{ID: l.KeyID, Alg: identity.AlgEd25519, PublicKey: l.PublicKey}
}

// uploadPin is the pin sync and the agent check on every hello, or nil
// for a lake with none.
func uploadPin(l config.Lake) *upload.Pin {
	if l.LakeID == "" || l.KeyID == "" || l.PublicKey == "" {
		return nil
	}
	return &upload.Pin{LakeID: l.LakeID, Key: pinOf(l)}
}

// refreshPin reads the lake's key list over a fresh nonce and moves the
// pin along the lake's endorsements to its newest active key, writing
// the move into config.json. It returns the lake with the pin it should
// use now and whether the pin moved. A list that cannot be trusted from
// the pin is a *pinRefused.
func refreshPin(ctx context.Context, env Env, l config.Lake) (config.Lake, bool, error) {
	pinned := pinOf(l)
	nonce, err := identity.NewNonce()
	if err != nil {
		return l, false, err
	}
	fctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	signed, p, err := upload.FetchKeys(fctx, l.Server.Value, nonce)
	if err != nil {
		return l, false, err
	}
	if p.LakeID != l.LakeID {
		return l, false, &pinRefused{fmt.Errorf("the key list is for lake %s, not the pinned %s", p.LakeID, l.LakeID)}
	}
	if p.Nonce != nonce {
		return l, false, &pinRefused{errors.New("the key list does not carry the nonce sent")}
	}
	if err := identity.VerifyKeyList(l.LakeID, pinned, signed, p.Keys); err != nil {
		return l, false, &pinRefused{err}
	}
	next, err := identity.Advance(l.LakeID, pinned, p.Keys, time.Now())
	if err != nil {
		return l, false, &pinRefused{err}
	}
	if next.ID == pinned.ID {
		return l, false, nil
	}
	moved := l
	moved.KeyID, moved.PublicKey = next.ID, next.PublicKey
	// The cached profile was verified under the old pin and may carry
	// only its signature. Replace it under the new pin before the pin
	// moves, so the lake's rules do not drop out between the two. If it
	// cannot be, the pin stays: the old key signs through the overlap,
	// and the next refresh tries again.
	d, err := fetchProfile(fctx, env, moved)
	if err != nil {
		return l, false, fmt.Errorf("the profile under key %s: %w; the pin stays on key %s until it can be fetched", next.ID, err, pinned.ID)
	}
	// The key list was checked against l, so the pin moves only in an
	// entry that still pins what l does. One replaced or re-pinned
	// while the list was fetched belongs to another identity. When the
	// entry named the server the list came from, it must still name it:
	// one pointed elsewhere may reach a copy of the lake that has not
	// seen the rotation. A flag or the environment chose the server
	// otherwise, and the entry's own is not what was fetched from.
	changed := func(lc config.LakeConfig) bool { return entryChanged(l, lc) }
	errChanged := fmt.Errorf("lake %s changed in config.json while its key list was fetched, so its pin does not move to key %s; the next refresh reads the new entry", l.Name, next.ID)
	// The check, the profile and the pin are one step under config.json's
	// lock. A register that replaced the entry meanwhile keeps the
	// profile it saved: nothing here writes over it.
	err = config.Locked(env.getenv, func(tx config.Tx) error {
		lc, ok, err := tx.Lake(l.Name)
		if err != nil {
			return err
		}
		if !ok || changed(lc) {
			return errChanged
		}
		restore, err := keepProfile(env, l)
		if err != nil {
			return err
		}
		if err := saveProfile(env, moved, d); err != nil {
			return restore(err)
		}
		// From here a failure leaves the pin on the old key, which may
		// not verify the profile just cached, so the old copy goes back.
		err = tx.UpdateLake(l.Name, func(lc *config.LakeConfig) error {
			if changed(*lc) {
				return errChanged
			}
			lc.KeyID, lc.PublicKey = next.ID, next.PublicKey
			return nil
		})
		if err != nil {
			return restore(err)
		}
		return nil
	})
	if err != nil {
		return l, false, err
	}
	return moved, true, nil
}

// keepProfile reads l's cached profile as it is now and returns a
// function that puts it back, or removes the cache if there was none,
// and joins any failure to do so onto the error it is given.
func keepProfile(env Env, l config.Lake) (func(error) error, error) {
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(lakestate.Dir(state, l.Name), lakeprofile.FileName)
	old, err := os.ReadFile(path)
	had := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("the cached profile: %w", err)
	}
	return func(cause error) error {
		var rerr error
		if had {
			rerr = config.WriteFileAtomic(path, old)
		} else if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			rerr = err
		}
		if rerr != nil {
			return errors.Join(cause, fmt.Errorf("the cached profile under key %s could not be put back, so the lake's rules are unavailable until the pin moves: %w", l.KeyID, rerr))
		}
		return cause
	}, nil
}

// fetchProfile fetches the lake's profile and verifies it under l's pin.
func fetchProfile(ctx context.Context, env Env, l config.Lake) (lakeprofile.Doc, error) {
	token, err := lakeToken(l)
	if err != nil {
		return lakeprofile.Doc{}, err
	}
	signed, err := upload.FetchAgentConfig(ctx, upload.Options{ServerURL: l.Server.Value, Token: token})
	if err != nil {
		return lakeprofile.Doc{}, err
	}
	return lakeprofile.Verify(signed, l)
}

// saveProfile caches d as l's profile.
func saveProfile(env Env, l config.Lake, d lakeprofile.Doc) error {
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	return lakeprofile.Save(lakestate.Dir(state, l.Name), d)
}

// saveProfileIfCurrent caches d in dir, under config.json's lock, while
// config.json's entry for l still pins what l does. An entry that
// register replaced since the agent read it belongs to another
// registration, whose profile register saved, and the agent reloads it.
func saveProfileIfCurrent(env Env, l config.Lake, dir string, d lakeprofile.Doc) error {
	return config.Locked(env.getenv, func(tx config.Tx) error {
		lc, ok, err := tx.Lake(l.Name)
		if err != nil {
			return err
		}
		if !ok || entryChanged(l, lc) {
			return fmt.Errorf("lake %s changed in config.json, so its profile is not saved; the agent reads the new entry when it reloads", l.Name)
		}
		return lakeprofile.Save(dir, d)
	})
}

// entryChanged reports whether config.json's entry lc is no longer the
// registration l was read from: another pin, device or lake, or, where
// the entry chose them, another server or token. A register --replace
// that kept the lake and its key still made a new device and token.
func entryChanged(l config.Lake, lc config.LakeConfig) bool {
	return lc.LakeID != l.LakeID || lc.KeyID != l.KeyID || lc.PublicKey != l.PublicKey || lc.DeviceID != l.DeviceID ||
		(l.Server.Source == config.SourceConfig && lc.Server != l.Server.Value) ||
		(l.TokenFile.Source == config.SourceConfig && lc.TokenFile != l.TokenFile.Value)
}
