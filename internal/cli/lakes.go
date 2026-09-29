package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakestate"
)

// errNoLake is what a command that talks to a lake says on a machine
// whose config.json has an empty lakes map.
var errNoLake = errors.New("no lake is configured: config.json has an empty lakes map")

// migrateDefault moves single-lake state into the default lake's
// directory, once. agentLocked says the caller holds agent.pid; a caller
// that does not takes it for the move, so an agent from before this
// release is not writing the files while they move.
func migrateDefault(env Env, state string, agentLocked bool) error {
	// Nothing at the top of the state directory: moved already, or a
	// fresh machine.
	has, err := lakestate.Legacy(state)
	if err != nil || !has {
		return err
	}
	// Legacy files are about to be moved, or, when lakes/default exists
	// because a crash came between the rename and the cleanup, removed.
	// Either way an agent from before this release may be writing them,
	// so the lock is taken first.
	if !agentLocked {
		release, err := writeAgentPID(state)
		if err != nil {
			return fmt.Errorf("sync state in %s is in the single-lake layout and an agent holds it: %w; stop that agent, then run this again", state, err)
		}
		defer release()
	}
	moved, err := lakestate.Migrate(state, config.DefaultLake)
	if err != nil {
		return err
	}
	if moved {
		fmt.Fprintf(env.stderr(), "terva-lampi: moved sync state to %s\n", lakestate.Dir(state, config.DefaultLake))
	}
	return nil
}

// captureDir is where status reads a lake's sync state: its own
// directory, or for the default lake before its first sync on this
// release, the single-lake files at the top of the state directory.
// status is a read and does not move them.
func captureDir(state string, lake config.Lake) (string, bool) {
	dir := lakestate.Dir(state, lake.Name)
	if lake.Name != config.DefaultLake {
		return dir, false
	}
	if _, err := os.Stat(dir); err == nil {
		return dir, false
	}
	if has, _ := lakestate.Legacy(state); has {
		return state, true
	}
	return dir, false
}

// lakeToken reads a lake's device token. A token file that no flag,
// variable or config names, and that does not exist, means no auth, which
// is a loopback lake started without --token-file. A named file must
// exist.
func lakeToken(l config.Lake) (string, error) {
	if l.TokenFile.Source == config.SourceDefault {
		if _, err := os.Stat(l.TokenFile.Value); os.IsNotExist(err) {
			return "", nil
		}
	}
	return auth.Read(l.TokenFile.Value)
}

// writeProfiles prints each lake's profile and where each machine-wide
// value came from: local for config.json, lake:NAME for that lake's
// profile, or default.
func writeProfiles(w io.Writer, lakes []config.Lake, cc clientConfig) {
	for _, l := range lakes {
		d, ok := cc.docs[l.Name]
		switch {
		case ok:
			fmt.Fprintf(w, "lake %s profile=%s version=%s issued=%s\n", l.Name, d.Payload.Profile, d.Payload.Version, d.Payload.IssuedAt.UTC().Format(time.RFC3339))
		case l.KeyID == "":
			// A token alone syncs, but fetches no profile. lakes adopt
			// pins the lake without registering it again.
			fmt.Fprintf(w, "lake %s profile=none (not registered: no pinned key; terva-lampi lakes adopt %s pins it)\n", l.Name, l.Name)
		default:
			fmt.Fprintf(w, "lake %s profile=none (not fetched yet)\n", l.Name)
		}
	}
	keys := make([]string, 0, len(cc.origins))
	for k := range cc.origins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		var value string
		switch {
		case strings.HasPrefix(k, "harnesses."):
			value = fmt.Sprintf("enabled=%t", cc.file.Harnesses.Enabled(strings.TrimPrefix(k, "harnesses.")))
		case k == "agent.debounce":
			window, _, _ := cc.file.Agent.Windows()
			value = window.String()
		case k == "agent.debounce_max":
			_, longest, _ := cc.file.Agent.Windows()
			value = longest.String()
		}
		fmt.Fprintf(w, "%s: %s source=%s\n", k, value, strings.ReplaceAll(cc.origins[k], " ", ":"))
	}
}

// writeLakes prints one line per lake for agent config.
func writeLakes(w io.Writer, lakes []config.Lake) {
	if len(lakes) == 0 {
		fmt.Fprintln(w, "lakes: none configured")
	}
	for _, l := range lakes {
		id := l.LakeID
		if id == "" {
			id = "-"
		}
		from := l.AllowFrom
		if from == "" {
			from = config.OriginLocal
		}
		fmt.Fprintf(w, "lake %s server=%s source=%s token_file=%s token_source=%s lake_id=%s projects_allow=%d projects_deny=%d allow_source=%s deny_source=%s\n",
			l.Name, l.Server.Value, l.Server.Source, l.TokenFile.Value, l.TokenFile.Source, id, len(l.Projects.Allow), len(l.Projects.Deny), strings.ReplaceAll(from, " ", ":"), strings.ReplaceAll(l.DenyFrom(), " ", ":"))
	}
}
