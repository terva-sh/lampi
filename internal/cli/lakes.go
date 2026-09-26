package cli

import (
	"fmt"
	"io"
	"os"

	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
)

// perLakeStateTicket is the work that gives each lake its own sync
// state. Until it lands, every lake would share one watermark store, and
// a push to a second lake would skip what the first already has.
const perLakeStateTicket = "TKT-01M3FP110 (Client state: per-lake directories and one-time legacy migration)"

// pushLake picks the one lake sync and the agent push to in this
// release: the default lake, which owns the existing state directory.
// Another lake is refused rather than pushed to with the default lake's
// watermarks. Other lakes are named on warn so they are not a silent
// no-op.
func pushLake(lakes []config.Lake, selected string, warn io.Writer, cmd string) (config.Lake, error) {
	if len(lakes) == 0 {
		return config.Lake{}, fmt.Errorf("no lake is configured")
	}
	first := lakes[0]
	if first.Name != config.DefaultLake {
		return config.Lake{}, fmt.Errorf("%s pushes only to the lake named %s until %s lands; lake %s has no state of its own yet", cmd, config.DefaultLake, perLakeStateTicket, first.Name)
	}
	if selected == "" && len(lakes) > 1 {
		fmt.Fprintf(warn, "terva-lampi %s: %d lakes configured; this release pushes to %s only\n", cmd, len(lakes), config.DefaultLake)
	}
	return first, nil
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
