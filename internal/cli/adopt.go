package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
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
	yes         bool
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
	in := bufio.NewReader(env.stdin())
	interactive := terminalStdin(env)

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
		if err := checkChange(env, file, l, applied.Profile, ok, d.Profile, true, o, in, interactive); err != nil {
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
	if err := confirmLake(env, in, interactive, o.fingerprint, server, lakeID, identity.Fingerprint(pub), "check 3, ", "Pin this lake and take its profile?", "changed"); err != nil {
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
	if err := checkChange(env, file, l, config.Profile{}, false, d.Profile, drop, o, in, interactive); err != nil {
		return err
	}

	// A single-lake state layout moves under lakes/default first, where
	// the profile is cached. The move needs no undo if the commit below
	// fails: sync and the agent make the same move for the default lake
	// at their next start, legacy config or not, and read its state
	// from lakes/default either way.
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

// checkChange lists what adopting l changes in what this machine
// uploads, and refuses a change nobody confirmed. Now is l with the
// profile it applies, applied, when it has one (hasApplied). After, the
// profile p applies, and with drop its allow rules replace the local
// ones. p's harness settings also fill what config.json leaves unset,
// machine-wide, so they change what every lake gets.
//
// What stops uploading is refused unless --force. What starts uploading
// is asked about on a terminal, or needs --yes without one: a profile
// written for other machines can allow more here than anyone meant.
func checkChange(env Env, file config.File, l config.Lake, applied config.Profile, hasApplied bool, p config.Profile, drop bool, o adoptOptions, in *bufio.Reader, interactive bool) error {
	if drop && len(p.Projects.Allow) == 0 && !o.force {
		return fmt.Errorf("the lake's profile allows no project, so dropping the %d local allow rules would stop every upload to lake %s; add rules to the profile first, or pass --force", len(l.Projects.Allow), l.Name)
	}
	v, err := viewAdopt(env, file, l, applied, hasApplied, p)
	if err != nil {
		return err
	}
	before := config.ApplyLakeProfile(l, applied, hasApplied)
	after := l
	if drop {
		after.Projects.Allow = nil
	}
	after = config.ApplyLakeProfile(after, p, true)
	w := env.stderr()

	// A harness turned off stops every project any lake uploads from it;
	// one turned on starts every project a lake would take from it. The
	// project rules are compared over the harnesses on both before and
	// after, so a project is listed once.
	var stopped, started []upload.RefusedProject
	if len(v.off) > 0 {
		rules := append([]config.Projects{before.Projects}, v.others...)
		stopped, err = harnessProjects(env, v.before, v.off, rules)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "lake %s's profile turns off %s, which this machine reads now, so every session of %s stops uploading to every lake\n", l.Name, strings.Join(v.off, ", "), pluralIt(len(v.off)))
		writeStopped(w, "uploaded now from "+strings.Join(v.off, ", "), stopped)
	}
	if len(v.on) > 0 {
		rules := append([]config.Projects{after.Projects}, v.others...)
		started, err = harnessProjects(env, v.after, v.on, rules)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "lake %s's profile turns on %s, which this machine does not read now\n", l.Name, strings.Join(v.on, ", "))
		writeStopped(w, "would start uploading from "+strings.Join(v.on, ", "), started)
	}
	var lost, gained []upload.RefusedProject
	if !reflect.DeepEqual(before.Projects, after.Projects) {
		both := config.Harnesses{}
		for id, h := range v.before {
			both[id] = h
		}
		for _, id := range append(append([]string{}, v.off...), v.on...) {
			both[id] = config.HarnessConfig{Enabled: false}
		}
		opt, err := readOnlyOptions(env, both)
		if err != nil {
			return err
		}
		var skipped []string
		lost, skipped = upload.Narrowed(opt, before.Projects, after.Projects)
		for _, s := range skipped {
			fmt.Fprintf(w, "terva-lampi: skipped %s\n", s)
		}
		gained, _ = upload.Narrowed(opt, after.Projects, before.Projects)
		if len(lost) > 0 {
			writeNarrowed(w, l.Name, lost)
		}
		if len(gained) > 0 {
			writeStopped(w, "lake "+l.Name+", refused now and allowed after", gained)
		}
	}

	if len(v.off) > 0 || len(lost) > 0 {
		if !o.force {
			var why []string
			if len(v.off) > 0 {
				why = append(why, fmt.Sprintf("%s would stop being read (set %s in config.json's harnesses to keep %s)", strings.Join(v.off, ", "), pluralIt(len(v.off)), pluralIt(len(v.off))))
			}
			if len(lost) > 0 {
				why = append(why, fmt.Sprintf("lake %s would refuse %d projects it uploads now (add rules for them to the profile)", l.Name, len(lost)))
			}
			return fmt.Errorf("after adopting, %s; all listed above. Pass --force to stop uploading them", strings.Join(why, ", and "))
		}
		fmt.Fprintln(w, "--force: going ahead; what is listed above as stopping stops uploading")
	}
	if len(started) > 0 || len(gained) > 0 {
		n := len(started) + len(gained)
		switch {
		case o.yes:
			fmt.Fprintln(w, "--yes: going ahead; what is listed above as starting starts uploading")
		case interactive:
			fmt.Fprintf(w, "%d projects listed above start uploading. Adopt anyway? [y/N] ", n)
			answer, _ := in.ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
				return errors.New("not confirmed; nothing was changed")
			}
		default:
			return fmt.Errorf("after adopting, %d projects this machine does not upload now would start uploading, listed above; narrow the profile's rules, or pass --yes to upload them", n)
		}
	}
	return nil
}

// adoptView is what adopting a lake changes machine-wide: the harness
// settings before and after, the harnesses turned off and on, and every
// other lake's project rules as they apply now.
type adoptView struct {
	before, after config.Harnesses
	off, on       []string
	others        []config.Projects
}

// viewAdopt computes an adoptView. Harness settings come from
// config.json first, then each pinned lake's cached profile in lake
// order; before, l applies applied when hasApplied, and after, p.
func viewAdopt(env Env, file config.File, l config.Lake, applied config.Profile, hasApplied bool, p config.Profile) (adoptView, error) {
	var v adoptView
	all, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{})
	if err != nil {
		return v, err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return v, err
	}
	order := make([]string, 0, len(all))
	profiles := map[string]config.Profile{}
	for _, other := range all {
		order = append(order, other.Name)
		if other.Name == l.Name {
			continue
		}
		prof, ok := config.Profile{}, false
		if lakeprofile.Pinned(other) {
			if d, found, err := lakeprofile.Load(lakestate.Dir(state, other.Name), other); err == nil && found {
				prof, ok = d.Profile, true
				profiles[other.Name] = prof
			}
		}
		v.others = append(v.others, config.ApplyLakeProfile(other, prof, ok).Projects)
	}
	if hasApplied {
		profiles[l.Name] = applied
	}
	before, _ := config.ApplyMachineProfiles(file, order, profiles)
	profiles[l.Name] = p
	after, _ := config.ApplyMachineProfiles(file, order, profiles)
	v.before, v.after = before.Harnesses, after.Harnesses
	for _, s := range knownSources() {
		id := s.harness.Name()
		switch on, next := v.before.Enabled(id), v.after.Enabled(id); {
		case on && !next:
			v.off = append(v.off, id)
		case !on && next:
			v.on = append(v.on, id)
		}
	}
	return v, nil
}

// harnessProjects reads only the harnesses ids, with their settings in
// hs, and returns each project with a session one of rules allows, once,
// counting every such session and checkout across the rules.
func harnessProjects(env Env, hs config.Harnesses, ids []string, rules []config.Projects) ([]upload.RefusedProject, error) {
	only := config.Harnesses{}
	for _, s := range knownSources() {
		only[s.harness.Name()] = config.HarnessConfig{Enabled: false}
	}
	for _, id := range ids {
		h := hs[id]
		h.Enabled = true
		only[id] = h
	}
	opt, err := readOnlyOptions(env, only)
	if err != nil {
		return nil, err
	}
	return upload.PermittedByAny(opt, rules), nil
}

func pluralIt(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// writeStopped lists projects that stop uploading with their harness.
func writeStopped(w io.Writer, label string, projects []upload.RefusedProject) {
	if len(projects) == 0 {
		fmt.Fprintf(w, "%s: no project\n", label)
		return
	}
	n := 0
	for _, p := range projects {
		n += p.Sessions
	}
	fmt.Fprintf(w, "%s: %d sessions in %d projects\n", label, n, len(projects))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SESSIONS\tHARNESS\tCWD\tGIT REMOTE")
	for _, p := range projects {
		cwd, remote := p.CWD, p.GitRemote
		if cwd == "" {
			cwd = "-"
		}
		if p.CWDs > 1 {
			cwd = fmt.Sprintf("%s (+%d more)", cwd, p.CWDs-1)
		}
		if remote == "" {
			remote = "-"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", p.Sessions, strings.Join(p.Harnesses, ","), cwd, remote)
	}
	tw.Flush()
}

func writeNarrowed(w io.Writer, lake string, lost []upload.RefusedProject) {
	writeRefused(w, "lake "+lake+", allowed now and refused by the profile", lost)
}
