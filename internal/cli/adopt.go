package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/identity"
	"terva.sh/lampi/internal/lakeprofile"
	"terva.sh/lampi/internal/lakestate"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

const (
	allowKeep    = "keep"
	allowProfile = "profile"
)

// adoptOptions are lakes adopt's flags.
type adoptOptions struct {
	fingerprint string
	allowFrom   string
	force       bool
}

// adoptLake pins a lake this machine already syncs to with a device
// token, so the agent takes the lake's profile, without registering
// again. The lake keeps its name, so the machine id, token and sync
// state stay, and nothing is sent twice. With --allow-from profile it
// also drops the lake's local allow rules, so the profile's apply, after
// checking that the switch refuses no project the local rules allow.
func adoptLake(env Env, name string, o adoptOptions) error {
	if o.allowFrom != allowKeep && o.allowFrom != allowProfile {
		return fmt.Errorf("--allow-from is %s or %s, not %q", allowKeep, allowProfile, o.allowFrom)
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	found, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{Lake: name})
	if err != nil {
		return err
	}
	l := found[0]
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	ctx := context.Background()

	if lakeprofile.Pinned(l) {
		if o.allowFrom != allowProfile {
			return fmt.Errorf("lake %s is already pinned to lake %s; nothing to adopt. To let its profile's allow rules apply, run terva-lampi lakes adopt %s --allow-from profile", l.Name, l.LakeID, l.Name)
		}
		if len(l.Projects.Allow) == 0 {
			fmt.Fprintf(env.stdout(), "lake %s has no local allow rules; its profile's allow rules already apply\n", l.Name)
			return nil
		}
		token, err := lakeToken(l)
		if err != nil {
			return err
		}
		d, err := fetchProfile(ctx, env, l)
		if err != nil {
			return fmt.Errorf("fetching lake %s's profile: %w", l.Name, err)
		}
		// Before the switch the agent applies the profile it cached, which
		// may be older than the one just fetched.
		applied, ok, err := lakeprofile.Load(lakestate.Dir(state, l.Name), l)
		if err != nil {
			return err
		}
		if err := checkNarrowing(env, file, l, applied.Profile, ok, d.Profile, true, o.force); err != nil {
			return err
		}
		// The profile checked is the one cached, so the agent that reloads
		// applies what the check approved.
		err = commitAdopt(env, l, token, d, func(lc *config.LakeConfig) {
			lc.Projects.Allow = nil
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "lake %s: removed %d local allow rules; profile %s allows %d\n", l.Name, len(l.Projects.Allow), d.Payload.Profile, len(d.Profile.Projects.Allow))
		fmt.Fprintln(env.stdout(), reloadAgent(state))
		return nil
	}

	server := l.Server.Value
	// The key is trusted from here on, so it must not be learned in the
	// clear from a host that is not this one.
	if err := upload.CheckToken(server, "x"); err != nil {
		return fmt.Errorf("lake %s is plain http to a host that is not loopback (%s); adopt pins a key only over https or loopback", l.Name, server)
	}
	token, err := lakeToken(l)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("lake %s has no device token at %s, so the lake does not know this machine; use terva-lampi register with a code instead", l.Name, l.TokenFile.Value)
	}

	// 1. The lake's key list, over a fresh nonce, signed by the key to pin.
	key, lakeID, err := adoptableKey(ctx, server)
	if err != nil {
		return fmt.Errorf("check 1, the lake's keys at %s: %w", server, err)
	}
	// 2. The lake that accepts this machine's token proves that key.
	pin := &upload.Pin{LakeID: lakeID, Key: key}
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, err = upload.HelloPinned(hctx, upload.Options{ServerURL: server, Token: token, Pin: pin})
	cancel()
	if err != nil {
		return fmt.Errorf("check 2, hello with this machine's token: %w", err)
	}
	// 3. A person says it is the lake they meant.
	pub, err := identity.ParsePublic(key)
	if err != nil {
		return err
	}
	in := bufio.NewReader(env.stdin())
	if err := confirmLake(env, in, terminalStdin(env), o.fingerprint, server, lakeID, identity.Fingerprint(pub), "check 3, ", "Pin this lake and take its profile?", "changed"); err != nil {
		return err
	}
	// 4. The profile the lake signs for this device verifies under the
	// pin. Its payload names the device, which a token-file device has
	// no other way to learn.
	pinned := l
	pinned.LakeID, pinned.KeyID, pinned.PublicKey = lakeID, key.ID, key.PublicKey
	d, err := fetchProfile(ctx, env, pinned)
	if err != nil {
		return fmt.Errorf("check 4, the lake's profile: %w", err)
	}
	pinned.DeviceID = d.Payload.DeviceID
	drop := o.allowFrom == allowProfile && len(l.Projects.Allow) > 0
	if err := checkNarrowing(env, file, l, config.Profile{}, false, d.Profile, drop, o.force); err != nil {
		return err
	}

	// A single-lake state layout moves under lakes/default first, where
	// the profile is cached.
	if l.Name == config.DefaultLake {
		if err := migrateDefault(env, state, false); err != nil {
			return err
		}
	}
	err = commitAdopt(env, l, token, d, func(lc *config.LakeConfig) {
		lc.LakeID, lc.KeyID, lc.PublicKey, lc.DeviceID = pinned.LakeID, pinned.KeyID, pinned.PublicKey, pinned.DeviceID
		if drop {
			lc.Projects.Allow = nil
		}
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "lake %s: pinned to lake %s, key %s", l.Name, lakeID, key.ID)
	if pinned.DeviceID != "" {
		fmt.Fprintf(env.stdout(), ", device %s", pinned.DeviceID)
	}
	fmt.Fprintf(env.stdout(), "; profile %s version %s\n", d.Payload.Profile, d.Payload.Version)
	switch {
	case drop:
		fmt.Fprintf(env.stdout(), "removed %d local allow rules; the profile's %d apply\n", len(l.Projects.Allow), len(d.Profile.Projects.Allow))
	case len(l.Projects.Allow) > 0:
		fmt.Fprintf(env.stdout(), "the %d local allow rules stay in force, so the profile's allow rules do not apply; terva-lampi lakes adopt %s --allow-from profile switches\n", len(l.Projects.Allow), l.Name)
	}
	fmt.Fprintln(env.stdout(), reloadAgent(state))
	return nil
}

// adoptableKey fetches the key list at server over a fresh nonce and
// returns the newest active key that signed it, with the list's lake id.
// Nothing is pinned yet, so the list is trusted only as far as its own
// signature: a person confirms the key's fingerprint next.
func adoptableKey(ctx context.Context, server string) (protocol.LakeKey, string, error) {
	nonce, err := identity.NewNonce()
	if err != nil {
		return protocol.LakeKey{}, "", err
	}
	fctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	signed, p, err := upload.FetchKeys(fctx, server, nonce)
	if err != nil {
		return protocol.LakeKey{}, "", err
	}
	if p.Nonce != nonce {
		return protocol.LakeKey{}, "", errors.New("the key list does not carry the nonce sent: it is a replay or a cache")
	}
	var active []protocol.LakeKey
	for _, k := range p.Keys {
		if k.Status == identity.StatusActive && !k.Compromised {
			active = append(active, k)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].Created.After(active[j].Created) })
	for _, k := range active {
		pub, err := identity.ParsePublic(k)
		if err != nil {
			continue
		}
		if identity.Verify(identity.ContextKeys, signed, pub) == nil {
			return k, p.LakeID, nil
		}
	}
	return protocol.LakeKey{}, "", errors.New("no active key in the list signed it")
}

// commitAdopt caches the verified profile d and rewrites l's entry with
// edit, under config.json's lock. It refuses, and writes nothing, when
// the entry or the token file's contents are no longer what the checks
// used: the pin and device id were checked for that token, and the
// narrowing for those allow rules. The profile goes in first, as
// register does it, so the agent that reloads the entry finds it; a
// failed write puts the old cached copy back.
func commitAdopt(env Env, l config.Lake, token string, d lakeprofile.Doc, edit func(*config.LakeConfig)) error {
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	changed := fmt.Errorf("lake %s changed in config.json or its token file while it was checked; nothing was written, run this again", l.Name)
	return config.Locked(env.getenv, func(tx config.Tx) error {
		now, err := lakeToken(l)
		if err != nil {
			return err
		}
		if now != token {
			return changed
		}
		restore, err := keepProfile(env, l)
		if err != nil {
			return err
		}
		if err := lakeprofile.Save(lakestate.Dir(state, l.Name), d); err != nil {
			return restore(err)
		}
		err = tx.AdoptLake(l.Name, l.Server.Value, func(lc *config.LakeConfig) error {
			if adoptChanged(l, *lc) {
				return changed
			}
			edit(lc)
			return nil
		})
		if err != nil {
			return restore(err)
		}
		return nil
	})
}

// adoptChanged reports whether config.json's entry lc is no longer the
// lake l that adopt checked: another pin, device, server or token, as
// entryChanged sees them, or other local allow rules.
func adoptChanged(l config.Lake, lc config.LakeConfig) bool {
	if entryChanged(l, lc) || len(lc.Projects.Allow) != len(l.Projects.Allow) {
		return true
	}
	for i := range lc.Projects.Allow {
		if lc.Projects.Allow[i] != l.Projects.Allow[i] {
			return true
		}
	}
	return false
}

// checkNarrowing refuses, unless force, an adoption that would stop
// uploading a project l's rules allow now. Now is l with the profile it
// applies, applied, when it has one (hasApplied). After, the profile p
// applies, and with drop its allow rules replace the local ones. Each
// such project is listed either way.
func checkNarrowing(env Env, file config.File, l config.Lake, applied config.Profile, hasApplied bool, p config.Profile, drop, force bool) error {
	if drop && len(p.Projects.Allow) == 0 && !force {
		return fmt.Errorf("the lake's profile allows no project, so dropping the %d local allow rules would stop every upload to lake %s; add rules to the profile first, or pass --force", len(l.Projects.Allow), l.Name)
	}
	before := config.ApplyLakeProfile(l, applied, hasApplied)
	after := l
	if drop {
		after.Projects.Allow = nil
	}
	after = config.ApplyLakeProfile(after, p, true)
	if reflect.DeepEqual(before.Projects, after.Projects) {
		return nil
	}
	opt, err := readOnlyOptions(env, file.Harnesses)
	if err != nil {
		return err
	}
	lost, skipped := upload.Narrowed(opt, before.Projects, after.Projects)
	for _, s := range skipped {
		fmt.Fprintf(env.stderr(), "terva-lampi: skipped %s\n", s)
	}
	if len(lost) == 0 {
		return nil
	}
	writeNarrowed(env.stderr(), l.Name, lost)
	if force {
		fmt.Fprintln(env.stderr(), "--force: going ahead; these stop uploading")
		return nil
	}
	return fmt.Errorf("after adopting, lake %s would refuse %d projects it uploads now, listed above; add rules for them to the profile, or pass --force to stop uploading them", l.Name, len(lost))
}

func writeNarrowed(w io.Writer, lake string, lost []upload.RefusedProject) {
	writeRefused(w, "lake "+lake+", allowed now and refused by the profile", lost)
}
