package cli

import (
	"fmt"
	"strings"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/redact"
)

const quarantineUsage = `terva-lampi quarantine — list and acknowledge redaction hits

usage:
  terva-lampi quarantine list
  terva-lampi quarantine allow <relpath|sha256>

sync and the agent scan each file with ruleset v2 before it leaves the
machine. A file with a hit is quarantined: it is not uploaded, and a
record is added to quarantine.jsonl in the state directory. The record
has the relpath, the sha256 of the bytes, the rules, and the hit count.
It does not have the matched text. A record is added once per relpath,
digest, and rule set, not on every sync.

list prints one line per record, oldest first:

  <time> <sha256> <relpath> hits=<n> rules=<rule,...> [manifest] [allowed]

allow acknowledges one digest. A 64-hex argument is that digest. Any
other argument is a relpath, and its newest record's digest is used.
The digest is written to quarantine_allow.json in the state directory.
The next sync uploads a file with exactly those bytes, stamped
redaction status override, the way redaction.upload_hits does for
every file. A file that changes has a new digest, is scanned again,
and is quarantined again until it is allowed again. A manifest record
is a hit in a path or project field, not in the file. It cannot be
allowed; rename the path or change the allowlist instead.
`

func runQuarantine(env Env, args []string) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(env.stdout(), quarantineUsage)
		return nil
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			fmt.Fprint(env.stdout(), quarantineUsage)
			return fmt.Errorf("unexpected argument %q", args[1])
		}
		return listQuarantine(env, state)
	case "allow":
		if len(args) != 2 {
			fmt.Fprint(env.stdout(), quarantineUsage)
			return fmt.Errorf("quarantine allow takes one relpath or sha256")
		}
		return allowQuarantine(env, state, args[1])
	default:
		fmt.Fprint(env.stdout(), quarantineUsage)
		return fmt.Errorf("unknown quarantine command %q", args[0])
	}
}

func listQuarantine(env Env, state string) error {
	recs, err := redact.ReadQuarantine(state)
	if err != nil {
		return err
	}
	allowed, err := redact.AllowedDigests(state)
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Fprintln(env.stderr(), "terva-lampi: nothing is quarantined")
		return nil
	}
	for _, r := range recs {
		line := fmt.Sprintf("%s %s %s hits=%d rules=%s",
			r.At.UTC().Format(time.RFC3339), r.SHA256, r.RelPath, r.Hits, strings.Join(r.Rules, ","))
		if r.Manifest {
			line += " manifest"
		}
		if allowed[r.SHA256] {
			line += " allowed"
		}
		fmt.Fprintln(env.stdout(), line)
	}
	return nil
}

func allowQuarantine(env Env, state, arg string) error {
	recs, err := redact.ReadQuarantine(state)
	if err != nil {
		return err
	}
	byDigest := protocol.ValidDigest(arg)
	var match *redact.Record
	for i := range recs {
		r := &recs[i]
		if (byDigest && r.SHA256 == arg) || (!byDigest && r.RelPath == arg) {
			match = r
		}
	}
	if match == nil {
		return fmt.Errorf("quarantine: no record for %q; run terva-lampi quarantine list", arg)
	}
	if match.Manifest {
		return fmt.Errorf("quarantine: %s: the hit is in the manifest, not the file, and cannot be allowed", match.RelPath)
	}
	if err := redact.Allow(state, redact.Allowed{SHA256: match.SHA256, RelPath: match.RelPath}); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "allowed %s %s\n", match.SHA256, match.RelPath)
	fmt.Fprintln(env.stdout(), "the next sync uploads these exact bytes with redaction status override; a changed file is scanned again")
	return nil
}
