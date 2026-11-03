package profiledoctor

import (
	"context"
	"errors"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

// Gather reads what Diagnose needs for profile name from the catalog,
// with p as its rules: the active devices that fetch the profile and
// take its allow rules, their newest inventories, and the hidden
// projects. p may be an edit not saved yet.
func Gather(ctx context.Context, cat *catalog.Catalog, name string, p config.Projects) (Input, error) {
	in := Input{Projects: p, Hidden: map[catalog.ProjectKey]bool{}}
	profile, err := cat.ProfileByName(ctx, name)
	if err != nil && !errors.Is(err, catalog.ErrNoProfile) {
		return in, err
	}
	in.Review = profile.Config.Review
	devices, err := cat.Devices(ctx)
	if err != nil {
		return in, err
	}
	reports, err := cat.DeviceReports(ctx)
	if err != nil {
		return in, err
	}
	local := map[string]bool{}
	for _, r := range reports {
		local[r.DeviceID] = r.Report.AllowSource == config.OriginLocal
	}
	for _, d := range devices {
		profile := d.Profile
		if profile == "" {
			profile = config.DefaultProfile
		}
		if d.State() != "active" || profile != name || local[d.ID] {
			continue
		}
		inv, ok, err := cat.DeviceInventoryOf(ctx, d.ID)
		if err != nil {
			return in, err
		}
		in.Devices = append(in.Devices, Device{Name: d.Name, Listed: ok, Projects: inv.Inventory.Projects})
	}
	hidden, err := cat.HiddenProjects(ctx)
	if err != nil {
		return in, err
	}
	for _, h := range hidden {
		in.Hidden[h.Key] = true
	}
	return in, nil
}
