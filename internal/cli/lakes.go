package cli

import (
	"fmt"
	"io"
	"os"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakestate"
)

// fanOutTicket is the work that pushes to every lake at once. Until it
// lands, sync and the agent push to one lake per run.
const fanOutTicket = "TKT-01M3FHHBP (Agent: fan out to many lakes)"

// pushLake picks the one lake sync and the agent push to in this
// release: the lake --lake names, else the default lake, else the first
// by name. Other lakes are named on warn so they are not a silent no-op.
func pushLake(lakes []config.Lake, selected string, warn io.Writer, cmd string) (config.Lake, error) {
	if len(lakes) == 0 {
		return config.Lake{}, fmt.Errorf("no lake is configured")
	}
	if selected == "" && len(lakes) > 1 {
		fmt.Fprintf(warn, "terva-lampi %s: %d lakes configured; this release pushes to %s only until %s lands\n", cmd, len(lakes), lakes[0].Name, fanOutTicket)
	}
	return lakes[0], nil
}

// prepareLake returns the lake's state directory and this machine's id
// for it. For the default lake it first moves single-lake state into
// that directory, once. agentLocked says the caller holds agent.pid; a
// caller that does not takes it for the move, so an agent from before
// this release is not writing the files while they move.
func prepareLake(env Env, state string, lake config.Lake, agentLocked bool) (string, config.Machine, error) {
	dir := lakestate.Dir(state, lake.Name)
	if lake.Name == config.DefaultLake {
		if err := migrateDefault(env, state, agentLocked); err != nil {
			return "", config.Machine{}, err
		}
	}
	m, err := config.EnsureLakeMachine(env.getenv, lake.Name)
	if err != nil {
		return "", config.Machine{}, err
	}
	return dir, m, nil
}

func migrateDefault(env Env, state string, agentLocked bool) error {
	if _, err := os.Stat(lakestate.Dir(state, config.DefaultLake)); err == nil {
		// Already moved. Migrate removes legacy files a crash left behind.
		_, err := lakestate.Migrate(state, config.DefaultLake)
		return err
	}
	has, err := lakestate.Legacy(state)
	if err != nil || !has {
		return err
	}
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
	for _, l := range lakes {
		id := l.LakeID
		if id == "" {
			id = "-"
		}
		fmt.Fprintf(w, "lake %s server=%s source=%s token_file=%s token_source=%s lake_id=%s projects_allow=%d projects_deny=%d\n",
			l.Name, l.Server.Value, l.Server.Source, l.TokenFile.Value, l.TokenFile.Source, id, len(l.Projects.Allow), len(l.Projects.Deny))
	}
}
