package web

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

// What a profile change does to the projects its devices hold: each
// device's newest inventory is read under the stored project rules and
// under the edited ones, and the projects whose verdict differs are
// listed. A wider rule can admit projects nobody looked at; this is
// where the operator sees them before saving.
//
// The lake sees only what the inventory says. A device whose config.json
// sets its own allow rules is not reached by the profile's, so it is
// left out, as the preview's LocalAllow count already says. A strict
// device lists only the projects it uploads, and a device that sent no
// inventory lists nothing; both are named, since the list cannot speak
// for what they would admit. A deny rule on the device itself is not
// visible either. A project the device refused under a deny rule is
// taken to be denied locally, and is never listed, only when the device
// says it has deny rules of its own, the inventory was made under the
// stored profile, and the stored profile's deny rules do not match the
// project. The inventory was made under the stored profile when the
// device reported applying it and the lake received the inventory after
// both that report and the save, as the review queue decides a copy is
// still refused. Short of that the refusal may come from an older
// profile, and the project is evaluated like any other: the list may
// then name a project the device denies itself, which errs toward
// showing the operator more.

// projectChange is one project a change admits or drops, across the
// devices whose newest inventory holds it.
type projectChange struct {
	Key      catalog.ProjectKey `json:"key"`
	Devices  []string           `json:"devices"`
	Sessions int                `json:"sessions"`
}

// profileReach is the projects a change to a profile's rules admits and
// drops, and the devices whose refused projects the lake cannot see.
type profileReach struct {
	Admits []projectChange `json:"admits"`
	Drops  []projectChange `json:"drops"`
	// Unlisted names the devices that send no list of refused projects:
	// strict ones, and ones that sent no inventory.
	Unlisted []string `json:"unlisted"`
}

// reach evaluates before and after, the stored and edited rules of a
// profile, against the newest inventory of each device that fetches it.
// stored is the stored profile: its version, and when it was saved,
// zero for one never saved.
func (s *Server) reach(ctx context.Context, before, after config.Projects, stored storedProfile, devices []profileDevice) (profileReach, error) {
	out := profileReach{Unlisted: []string{}}
	admits := map[catalog.ProjectKey]*projectChange{}
	drops := map[catalog.ProjectKey]*projectChange{}
	for _, d := range devices {
		if d.AllowSource == config.OriginLocal {
			continue
		}
		inv, ok, err := s.catalog.DeviceInventoryOf(ctx, d.ID)
		if err != nil {
			return profileReach{}, err
		}
		if !ok || inv.Inventory.Mode == protocol.InventoryStrict {
			out.Unlisted = append(out.Unlisted, d.Name)
		}
		if !ok {
			continue
		}
		madeUnderStored := d.Applied == stored.Version && inv.Received.After(stored.Saved) && inv.Received.After(d.Reported)
		denyLocal := madeUnderStored && slices.Contains(strings.Split(d.DenySource, "+"), config.OriginLocal)
		for _, p := range inv.Inventory.Projects {
			k, ok := catalog.ProjectKeyOf(p)
			if !ok {
				continue
			}
			id := config.ProjectID{CWD: p.CWD, CWDHash: p.CWDHash, GitRemote: p.GitRemote}
			if denyLocal && !p.Allowed && p.Reason == config.RefusedByDeny && (config.Projects{Deny: before.Deny}).Refusal(id) != config.RefusedByDeny {
				continue
			}
			was, is := before.Permitted(id), after.Permitted(id)
			if was == is {
				continue
			}
			into := admits
			if was {
				into = drops
			}
			c := into[k]
			if c == nil {
				c = &projectChange{Key: k}
				into[k] = c
			}
			if !slices.Contains(c.Devices, d.Name) {
				c.Devices = append(c.Devices, d.Name)
			}
			c.Sessions += p.Sessions
		}
	}
	out.Admits, out.Drops = sortedChanges(admits), sortedChanges(drops)
	return out, nil
}

// storedProfile is what reach needs of the stored profile.
type storedProfile struct {
	Version string
	Saved   time.Time
}

// sortedChanges is the changes, most sessions first, then by key.
func sortedChanges(m map[catalog.ProjectKey]*projectChange) []projectChange {
	out := make([]projectChange, 0, len(m))
	for _, c := range m {
		slices.Sort(c.Devices)
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b projectChange) int {
		if c := cmp.Compare(b.Sessions, a.Sessions); c != 0 {
			return c
		}
		return cmp.Or(cmp.Compare(a.Key.Kind, b.Key.Kind), cmp.Compare(a.Key.Key, b.Key.Key))
	})
	return out
}
