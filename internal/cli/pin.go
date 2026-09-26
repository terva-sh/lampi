package cli

import (
	"context"
	"errors"
	"fmt"
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
	// moves, so the lake's rules do not drop out between the two.
	if err := refetchProfile(fctx, env, moved); err != nil {
		fmt.Fprintf(env.stderr(), "terva-lampi: lake %s: profile under the new key: %v; the agent fetches it again\n", l.Name, err)
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return l, false, err
	}
	lc, ok := file.Lakes[l.Name]
	if !ok {
		return l, false, fmt.Errorf("lake %s is not in the lakes map, so its pin cannot move", l.Name)
	}
	lc.KeyID, lc.PublicKey = next.ID, next.PublicKey
	if err := config.SetLake(env.getenv, l.Name, lc); err != nil {
		return l, false, err
	}
	return moved, true, nil
}

// refetchProfile fetches the lake's profile, verifies it under l's pin
// and caches it.
func refetchProfile(ctx context.Context, env Env, l config.Lake) error {
	token, err := lakeToken(l)
	if err != nil {
		return err
	}
	signed, err := upload.FetchAgentConfig(ctx, upload.Options{ServerURL: l.Server.Value, Token: token})
	if err != nil {
		return err
	}
	d, err := lakeprofile.Verify(signed, l)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	return lakeprofile.Save(lakestate.Dir(state, l.Name), d)
}
