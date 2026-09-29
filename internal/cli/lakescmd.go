package cli

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakestate"
)

const lakesUsage = `terva-lampi lakes — list or remove the lakes this machine reports to

usage:
  terva-lampi lakes [list]
  terva-lampi lakes remove NAME [--purge-state]
  terva-lampi lakes adopt [NAME] [--fingerprint SHA256:...]
                          [--allow-from keep|profile] [--force]

list prints one line per lake in config.json, with its server, token
file, pinned lake id and allowlist counts, then its base configuration.

remove deletes lakes.NAME from config.json and the token file register
wrote for it, and tells a running agent to reload, which drains that
lake's outbox and stops pushing to it. On Windows the agent must be
restarted. A token file another lake names as its token_file is kept. The lake still lists the device; its operator revokes it
with serve devices revoke. Removing the last lake leaves the agent
running with no lake.

The lake's sync state in lakes/NAME/ in the state directory is kept, so
registering it again sends nothing twice. --purge-state deletes it
too; while an agent is running it refuses and changes nothing. The
default lake set by the top-level server in config.json is not in the
lakes map; edit config.json for that one.

adopt pins a lake this machine already syncs to with a device token,
such as one set by the top-level server and token_file, so the agent
takes the lake's profile without registering again. NAME defaults to
default. It checks, in order, and stops at the first check that fails:

  1. the key list at the lake's URL, fetched over a fresh nonce, is
     signed by an active key; the URL is https, or http to loopback;
  2. the lake that accepts this machine's token proves that key in
     hello, over a fresh nonce;
  3. you confirm the URL, lake id and key fingerprint, on a terminal or
     with --fingerprint set to what serve identity prints on the lake
     host;
  4. the profile the lake signs for this device verifies under that key.

Then it caches the profile and writes the pin, and the device id the
profile names, into the lake's entry. A default lake set by the
top-level server and token_file moves into lakes.default with its
projects.allow; top-level projects.deny stays. The lake keeps its name,
machine id, token and sync state, so nothing is sent again and the lake
lists the same device. A running agent is told to reload.

A lake's local allow rules shut out its profile's. --allow-from keep,
the default, leaves them in force. --allow-from profile removes them,
so the profile's allow rules apply; on a lake already pinned it does
only that. The profile's deny rules and harness settings apply either
way. Before it writes anything, adopt lists each harness the profile
would turn off and each project the change would stop uploading,
reading every session the agent would read, and refuses while there is
one, unless --force.
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
	if sub == "adopt" {
		name = config.DefaultLake
		if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
			name, args = args[0], args[1:]
		}
	}
	if sub == "remove" {
		if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
			fmt.Fprint(env.stdout(), lakesUsage)
			return errors.New("lakes remove needs a lake name")
		}
		name, args = args[0], args[1:]
	}
	var purge bool
	adopt := adoptOptions{allowFrom: allowKeep}
	rest, err := parseFlags(env, args, lakesUsage, func(fs *flag.FlagSet) {
		switch sub {
		case "remove":
			fs.BoolVar(&purge, "purge-state", false, "delete the lake's sync state too")
		case "adopt":
			fs.StringVar(&adopt.fingerprint, "fingerprint", "", "the lake key fingerprint serve identity prints")
			fs.StringVar(&adopt.allowFrom, "allow-from", allowKeep, "keep the local allow rules, or use the profile's")
			fs.BoolVar(&adopt.force, "force", false, "adopt even if a project stops uploading")
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
	case "adopt":
		if !config.ValidLakeName(name) {
			return fmt.Errorf("%q is not a lake name; terva-lampi lakes lists them", name)
		}
		return adoptLake(env, name, adopt)
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
	// A token that cannot be removed is still a credential for the
	// lake. The rest of the removal goes ahead, and the command fails
	// at the end naming the file.
	var tokenErr error
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
		tokenPath := tokensPath(dir, name)
		if lc.TokenFile == "" || cleanTokenPath(lc.TokenFile) == cleanTokenPath(tokenPath) {
			// Another lake may still name the file as its token_file.
			// Deleting it would lock that lake out, so it is kept.
			sharers, err := remainingTokenSharers(env, name, tokenPath)
			if err != nil {
				fmt.Fprintf(env.stdout(), "kept %s, since whether another lake names it cannot be checked: %v\n", tokenPath, err)
			} else if len(sharers) > 0 {
				fmt.Fprintf(env.stdout(), "kept %s, which lake %s also names as its token_file\n", tokenPath, strings.Join(sharers, ", "))
			} else {
				err := os.Remove(tokenPath)
				switch {
				case err == nil:
					fmt.Fprintf(env.stdout(), "removed %s\n", tokenPath)
				case !errors.Is(err, fs.ErrNotExist):
					tokenErr = fmt.Errorf("lake %s is removed from config.json, but its token is still in %s: %w; delete that file by hand", name, tokenPath, err)
				}
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
		return tokenErr
	}
	if err := os.RemoveAll(stateDir); err != nil {
		return errors.Join(tokenErr, err)
	}
	fmt.Fprintf(env.stdout(), "removed sync state %s\n", stateDir)
	return tokenErr
}

func tokensPath(configDir, name string) string {
	return filepath.Join(configDir, "tokens", name+".token")
}

// remainingTokenSharers names the lakes other than name that config.json
// now resolves to the token file path.
func remainingTokenSharers(env Env, name, path string) ([]string, error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return nil, err
	}
	lakes, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{})
	if err != nil {
		return nil, err
	}
	return tokenSharers(lakes, name, path), nil
}

// tokenSharers names the lakes other than name whose token file is path,
// after each resolved the way the agent reads it.
func tokenSharers(lakes []config.Lake, name, path string) []string {
	var names []string
	for _, l := range lakes {
		if l.Name != name && l.TokenFile.Value != "" && cleanTokenPath(l.TokenFile.Value) == cleanTokenPath(path) {
			names = append(names, l.Name)
		}
	}
	return names
}

// cleanTokenPath makes token paths comparable. A relative one is opened
// from the working directory, so it is made absolute from there.
func cleanTokenPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
