package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakestate"
)

const lakesUsage = `terva-lampi lakes — list or remove the lakes this machine reports to

usage:
  terva-lampi lakes [list]
  terva-lampi lakes remove NAME [--purge-state]

list prints one line per lake in config.json, with its server, token
file, pinned lake id and allowlist counts, then its base configuration.

remove deletes lakes.NAME from config.json and the token file register
wrote for it, and tells a running agent to reload, which drains that
lake's outbox and stops pushing to it. On Windows the agent must be
restarted. The lake still lists the device; its operator revokes it
with serve devices revoke. Removing the last lake leaves the agent
running with no lake.

The lake's sync state in lakes/NAME/ in the state directory is kept, so
registering it again sends nothing twice. --purge-state deletes it
too; while an agent is running it refuses and changes nothing. The
default lake set by the top-level server in config.json is not in the
lakes map; edit config.json for that one.
`

func runLakes(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), lakesUsage)
		return nil
	}
	sub := "list"
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		sub, args = args[0], args[1:]
	}
	var name string
	if sub == "remove" {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			fmt.Fprint(env.stdout(), lakesUsage)
			return errors.New("lakes remove needs a lake name")
		}
		name, args = args[0], args[1:]
	}
	var purge bool
	rest, err := parseFlags(env, args, lakesUsage, func(fs *flag.FlagSet) {
		if sub == "remove" {
			fs.BoolVar(&purge, "purge-state", false, "delete the lake's sync state too")
		}
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), lakesUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	switch sub {
	case "list":
		cc, err := loadClientConfig(env, env.stderr(), config.LakeFlags{})
		if err != nil {
			return err
		}
		writeLakes(env.stdout(), cc.lakes)
		if len(cc.lakes) > 0 {
			writeProfiles(env.stdout(), cc.lakes, clientConfig{docs: cc.docs, file: cc.file})
		}
		return nil
	case "remove":
		return removeLake(env, name, purge)
	default:
		fmt.Fprint(env.stdout(), lakesUsage)
		return fmt.Errorf("unknown lakes command %q", sub)
	}
}

func removeLake(env Env, name string, purge bool) error {
	// The name becomes a path under the state directory that --purge-state
	// deletes, so a name like .. must not get that far.
	if !config.ValidLakeName(name) {
		return fmt.Errorf("%q is not a lake name: lowercase letters, digits, '-' and '_', at most 32 characters; terva-lampi lakes lists them", name)
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	lc, inMap := file.Lakes[name]
	stateDir := lakestate.Dir(state, name)
	if !inMap {
		if name == config.DefaultLake && (file.Lakes == nil || file.Server != "" || file.TokenFile != "") {
			return errors.New("the default lake is set by the top-level server or token_file in config.json, not the lakes map; edit config.json to remove it")
		}
		// A lake removed earlier can still have its state purged.
		if _, err := os.Stat(stateDir); !purge || err != nil {
			return fmt.Errorf("no lake named %s in config.json; terva-lampi lakes lists them", name)
		}
	}
	// The running agent may still be draining this lake. Purge only
	// while no agent holds agent.pid, and take it before changing
	// anything, so a refusal leaves the lake as it was.
	if purge {
		release, err := writeAgentPID(state)
		if err != nil {
			return fmt.Errorf("an agent is running, so nothing was removed; stop the agent and run terva-lampi lakes remove %s --purge-state again, or drop --purge-state to keep the sync state in %s", name, stateDir)
		}
		defer release()
	}
	if inMap {
		if err := config.RemoveLake(env.getenv, name); err != nil {
			return err
		}
		fmt.Fprintf(env.stdout(), "removed lake %s from config.json\n", name)
		// The token file register writes is removed with the lake. One
		// the entry names somewhere else was placed by hand and is kept.
		dir, err := config.ConfigDir(env.getenv)
		if err != nil {
			return err
		}
		if lc.TokenFile == "" || lc.TokenFile == tokensPath(dir, name) {
			if err := os.Remove(tokensPath(dir, name)); err == nil {
				fmt.Fprintf(env.stdout(), "removed %s\n", tokensPath(dir, name))
			}
		} else {
			fmt.Fprintf(env.stdout(), "kept %s, which config.json named\n", lc.TokenFile)
		}
		if purge {
			// This process holds agent.pid; reloadAgent would find it
			// and signal this process.
			fmt.Fprintln(env.stdout(), "no agent is running; the next one to start reads the lakes")
		} else {
			fmt.Fprintln(env.stdout(), reloadAgent(state))
		}
		if lc.LakeID != "" {
			fmt.Fprintf(env.stdout(), "the lake still lists this machine's device; its operator removes it with serve devices revoke\n")
		}
	}
	if !purge {
		fmt.Fprintf(env.stdout(), "kept sync state in %s\n", stateDir)
		return nil
	}
	if err := os.RemoveAll(stateDir); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "removed sync state %s\n", stateDir)
	return nil
}

func tokensPath(configDir, name string) string {
	return filepath.Join(configDir, "tokens", name+".token")
}
