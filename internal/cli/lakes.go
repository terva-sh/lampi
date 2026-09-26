package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

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
		fmt.Fprintf(w, "lake %s server=%s source=%s token_file=%s token_source=%s lake_id=%s projects_allow=%d projects_deny=%d\n",
			l.Name, l.Server.Value, l.Server.Source, l.TokenFile.Value, l.TokenFile.Source, id, len(l.Projects.Allow), len(l.Projects.Deny))
	}
}
