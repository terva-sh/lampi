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
  terva-lampi serve devices set-profile NAME PROFILE [--data DIR]

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

set-profile chooses the profile in the lake's catalog that the device's
agent fetches from GET /v1/agent/config. The agent picks it up at its
next fetch. A profile the catalog does not hold is refused.
set-profile NAME default goes back to the default.

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
	var data string
	rest, err := parseFlags(env, args, devicesUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
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
		cat, err := catalog.OpenCurrent(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		now := time.Now()
		var d catalog.Device
		actor := "serve devices " + sub
		if sub == "revoke" {
			d, err = cat.RevokeDevice(context.Background(), name, actor, now)
		} else {
			d, err = cat.UnbindDevice(context.Background(), name, actor, now)
		}
		if errors.Is(err, catalog.ErrNoDevice) {
			return fmt.Errorf("no device named %s; serve devices list shows them", name)
		}
		if err != nil {
			return err
		}
		// The change is committed. Say so before anything else, so a
		// failed audit write is not read as a revoke that did not happen.
		done := map[string]string{"revoke": "revoked", "unbind": "unbound"}[sub]
		fmt.Fprintf(env.stdout(), "%s %s\n", done, d.Name)
		// The event committed with the change. A line that cannot be
		// written now stays queued, and the next serve or serve command
		// writes it: running the command again is not how to retry it.
		if err := cat.FlushAudit(context.Background(), data); err != nil {
			return fmt.Errorf("%s %s, but writing it to %s failed: %w; the change stands, and the line stays queued in the catalog until the audit log can be written: do not run %s again to retry it", done, d.Name, audit.FileName, err, sub)
		}
		return nil
	case "set-profile":
		stored := profile
		if stored == config.DefaultProfile {
			stored = ""
		}
		cat, err := catalog.OpenCurrent(path)
		if err != nil {
			return err
		}
		defer cat.Close()
		known, err := cat.HasProfile(context.Background(), profile)
		if err != nil {
			return err
		}
		if !known {
			return fmt.Errorf("no profile named %s in the lake's catalog", profile)
		}
		now := time.Now()
		d, err := cat.SetDeviceProfile(context.Background(), name, stored, profile, "serve devices set-profile", now)
		if errors.Is(err, catalog.ErrNoDevice) {
			return fmt.Errorf("no device named %s; serve devices list shows them", name)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "set %s to profile %s\n", d.Name, profile)
		if err := cat.FlushAudit(context.Background(), data); err != nil {
			return fmt.Errorf("set %s to profile %s, but writing it to %s failed: %w; the change stands, and the line stays queued until the audit log can be written", d.Name, profile, audit.FileName, err)
		}
		return nil
	default:
		fmt.Fprint(env.stdout(), devicesUsage)
		return fmt.Errorf("unknown serve devices command %q", sub)
	}
}
