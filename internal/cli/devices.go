package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"terva.sh/lampi/internal/audit"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
)

const devicesUsage = `terva-lampi serve devices — list, revoke, or unbind devices

usage:
  terva-lampi serve devices [list] [--data DIR]
  terva-lampi serve devices revoke NAME [--data DIR]
  terva-lampi serve devices unbind NAME [--data DIR]
  terva-lampi serve devices set-profile NAME PROFILE [--data DIR] [--profiles PATH]

list prints one line per device: name, state, source, profile, bound
machine_id, id, and when it was made. state is active, revoked, or detached (a
token-file device whose token has left the file).

revoke stops the device's token on the lake's next request. serve does
not need a restart or a signal. Revoking is final: the token does not
come back by staying in, or returning to, the token file. Remove it from
the file as well.

unbind clears the machine_id the device is bound to, so its next
manifest binds it again. Use it when a machine was reinstalled and has a
new machine id.

set-profile chooses the profile in the profiles file (--profiles, or
profiles.json in the lake directory) that the device's agent fetches
from GET /v1/agent/config. The agent picks it up at its next fetch.
A profile the file does not hold is refused. set-profile NAME default
goes back to the default.

All four run while serve runs. revoke, unbind and set-profile write to
the catalog and append to audit.jsonl in the lake directory.
`

func runServeDevices(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), devicesUsage)
		return nil
	}
	sub := "list"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		sub, args = args[0], args[1:]
	}
	var name, profile string
	if sub == "revoke" || sub == "unbind" || sub == "set-profile" {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			fmt.Fprint(env.stdout(), devicesUsage)
			return fmt.Errorf("serve devices %s needs a device name", sub)
		}
		name, args = args[0], args[1:]
	}
	if sub == "set-profile" {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			fmt.Fprint(env.stdout(), devicesUsage)
			return fmt.Errorf("serve devices set-profile needs a device name and a profile")
		}
		profile, args = args[0], args[1:]
	}
	var data, profilesFile string
	rest, err := parseFlags(env, args, devicesUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&profilesFile, "profiles", "", "agent profiles file (default: profiles.json in the lake directory)")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), devicesUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data, err = lakeDir(env, data); err != nil {
		return err
	}
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("serve devices: %w", err)
	}
	switch sub {
	case "list":
		cat, err := catalog.OpenReadOnly(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		devices, err := cat.Devices(context.Background())
		if err != nil {
			return err
		}
		for _, d := range devices {
			machine := d.MachineID
			if machine == "" {
				machine = "-"
			}
			prof := d.Profile
			if prof == "" {
				prof = config.DefaultProfile
			}
			fmt.Fprintf(env.stdout(), "%s %s %s profile=%s machine=%s id=%s created=%s\n",
				d.Name, d.State(), d.Source, prof, machine, d.ID, d.Created.UTC().Format(time.RFC3339))
		}
		if len(devices) == 0 {
			fmt.Fprintln(env.stdout(), "no devices; serve records its token file's devices when it starts")
		}
		return nil
	case "revoke", "unbind":
		cat, err := catalog.Open(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		now := time.Now()
		var d catalog.Device
		kind := audit.DeviceUnbound
		if sub == "revoke" {
			kind = audit.DeviceRevoked
			d, err = cat.RevokeDevice(context.Background(), name, now)
		} else {
			d, err = cat.UnbindDevice(context.Background(), name)
		}
		if errors.Is(err, catalog.ErrNoDevice) {
			return fmt.Errorf("no device named %s; serve devices list shows them", name)
		}
		if err != nil {
			return err
		}
		// The change is committed. Say so before anything else, so a
		// failed audit write is not read as a revoke that did not happen.
		fmt.Fprintf(env.stdout(), "%sd %s\n", sub, d.Name)
		if err := audit.Append(data, audit.Event{Time: now, Kind: kind, Device: d.Name, DeviceID: d.ID, MachineID: d.MachineID, Actor: "serve devices " + sub}); err != nil {
			// revoke is final, so running it again changes nothing but the
			// record. unbind is not: the device may have bound again since,
			// and a second unbind would clear that binding.
			if sub == "revoke" {
				return fmt.Errorf("revoked %s, but writing it to %s failed: %w; the change stands, and running revoke again only retries the record", d.Name, audit.FileName, err)
			}
			return fmt.Errorf("unbound %s (it was bound to machine %s), but writing it to %s failed: %w; the change stands. Do not run unbind again to retry the record: the device may have bound again since, and a second unbind would clear that", d.Name, d.MachineID, audit.FileName, err)
		}
		return nil
	case "set-profile":
		if profilesFile == "" {
			profilesFile = filepath.Join(data, config.ProfilesFileName)
		}
		profiles, err := config.LoadProfiles(profilesFile)
		if err != nil {
			return err
		}
		if _, ok := profiles[profile]; !ok {
			return fmt.Errorf("no profile named %s in %s", profile, profilesFile)
		}
		stored := profile
		if stored == config.DefaultProfile {
			stored = ""
		}
		cat, err := catalog.Open(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		now := time.Now()
		d, err := cat.SetDeviceProfile(context.Background(), name, stored)
		if errors.Is(err, catalog.ErrNoDevice) {
			return fmt.Errorf("no device named %s; serve devices list shows them", name)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "set %s to profile %s\n", d.Name, profile)
		if err := audit.Append(data, audit.Event{Time: now, Kind: audit.DeviceProfile, Device: d.Name, DeviceID: d.ID, MachineID: d.MachineID, Actor: "serve devices set-profile", Detail: "profile=" + profile}); err != nil {
			return fmt.Errorf("set %s to profile %s, but writing it to %s failed: %w; the change stands", d.Name, profile, audit.FileName, err)
		}
		return nil
	default:
		fmt.Fprint(env.stdout(), devicesUsage)
		return fmt.Errorf("unknown serve devices command %q", sub)
	}
}
