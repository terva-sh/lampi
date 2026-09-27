package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/upload"
)

const syncUsage = `terva-lampi sync — push new bytes once

usage:
  terva-lampi sync [--lake NAME] [--server URL] [--token-file PATH]

Walks terva sessions, Claude Code projects/**/*.jsonl, Codex
rollout-*.jsonl, OpenCode export JSON, Cursor IDE state.vscdb
snapshots, and Cursor CLI store.db snapshots. history.jsonl is not
a Codex rollout. A project is uploaded
only when config.json allowlists it, by cwd prefix, git remote, or cwd
hash. Anything else is refused. Deny rules win. An empty allow list
refuses everything. Claude Code uses $CLAUDE_CONFIG_DIR, or ~/.claude
when that is unset. Codex uses $CODEX_HOME, or ~/.codex. OpenCode uses
$XDG_DATA_HOME/opencode/export, or ~/.local/share/opencode/export. When
that directory has no JSON, the database file at the data-directory
root is considered instead. The WAL sidecar is not. A database file
has no session directory, so the allowlist refuses it. Cursor IDE
state is read from the user-data directory: $XDG_CONFIG_HOME/Cursor
or ~/.config/Cursor on Linux, ~/Library/Application Support/Cursor on
macOS, and %APPDATA%\Cursor on Windows. The upload is a JSON export of
a snapshot. Keys under cursorAuth/ are removed. The raw database is
not uploaded. The global database has an empty cwd and is refused
by design. A workspace export copies that global database read-only
and merges cursorDiskKV rows for composers named by that workspace's
composer.composerHeaders. The session id stays workspace/<id>. A
workspace database takes its cwd from workspace.json.
The Cursor CLI store is separate. Its config directory is
$CURSOR_CONFIG_DIR, or $XDG_CONFIG_HOME/cursor on Linux when that
variable is set, otherwise ~/.cursor on macOS and Linux and the
.cursor directory under USERPROFILE on Windows. Each chat is
chats/<workspace>/<session>/store.db. The upload is a JSON export
of a snapshot. It is not an IDE session and it does not share the
IDE watermark. Keys under cursorAuth/ are removed. The raw database
is not uploaded. The export needs an absolute cwd in the sibling
meta.json. A relative path or a file URI is an empty cwd, and the
allowlist refuses it. The workspace hash is not a path. A refused
cursor or cursor-cli session with an empty cwd is named on stderr
with that reason. The projects allow and deny rules are unchanged.

config.json harnesses can set enabled false, which skips that
harness, or root, an absolute path that replaces the environment
variable and the default. The id is terva, claude, codex, opencode,
cursor, or cursor-cli. Omit the map or the id and the harness stays
on. A skipped harness does not move its watermark and does not
upload. There is no per-harness root flag.

Ruleset v2 scans each file before the lake is contacted. A hit is
quarantined under the state directory and is not uploaded, unless
redaction.upload_hits is set or terva-lampi quarantine allow
acknowledged that file's exact digest. See terva-lampi quarantine
--help.

A session whose files all match their watermarks is not read,
scanned, or posted, and the line ends in unchanged <n>. The watermark
moves only after the lake ACKs the manifest. Pending digests sit in
the outbox until that ACK. A grown file uploads only the new tail.
byte_watermark_prev is the previous length and tail_sha256 is the
hash of those bytes. The lake assembles the tail onto the stored
prefix. When the bytes before the tail scanned clean, the scan covers
the tail and 64 KiB before it. A clock that disagrees with
hello's server_time by more than five minutes is warned about and the
push still runs.

hello runs before the files are read and scanned. A blob over 4 MiB goes as Content-Range
pieces. No timeout covers a whole request; one that moves no bytes for
60s is cancelled.

--lake names a lake from the lakes map in config.json. Without it,
sync pushes to every lake in turn, the lake named default first, and
labels each line with its lake when there is more than one. A lake that
fails does not stop the next, and sync exits non-zero naming the lakes
that failed. An interrupt stops the run, and the lakes it did not reach
count as failed. --server and --token-file override one lake's values: the
one --lake names, or the only one. Each lake keeps its own sync state in
lakes/<name>/ in the state directory. The first sync on this release
moves single-lake state there; it waits for no agent, and refuses while
an older agent holds it.

--server defaults to LAMPI_SERVER, then the URL in config.json, or
http://127.0.0.1:8787. --token-file defaults to LAMPI_TOKEN_FILE, then
the token path in config.json, then the token file in the config
directory. The agent and status use the same order. The token is read
from a file, never from an argument. It is sent over
https, or over http only to localhost, 127.0.0.0/8, or ::1. Anything
else is refused before the scan.
`

// syncLake pushes to one lake. Tests replace it.
var syncLake = upload.Sync

func runSync(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), syncUsage)
		return nil
	}
	var serverFlag, tokenFlag, lakeFlag string
	rest, err := parseFlags(env, args, syncUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lakeFlag, "lake", "", "lake name from config.json")
		fs.StringVar(&serverFlag, "server", "", "lake base URL")
		fs.StringVar(&tokenFlag, "token-file", "", "device token file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), syncUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	cc, err := loadClientConfig(env, env.stderr(), config.LakeFlags{Lake: lakeFlag, Server: serverFlag, TokenFile: tokenFlag})
	if err != nil {
		return err
	}
	file, lakes := cc.file, cc.lakes
	if len(lakes) == 0 {
		return errNoLake
	}
	src, err := sources(env.getenv, file.Harnesses)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Each lake is pushed in turn with its own state. One that fails,
	// including one that cannot be prepared, does not stop the next; with
	// several, each line names its lake.
	var failed []string
	var last error
	fail := func(lake config.Lake, label string, err error) {
		last = err
		failed = append(failed, lake.Name)
		if len(lakes) > 1 {
			fmt.Fprintf(env.stderr(), "terva-lampi: %s%v\n", label, err)
		}
	}
	for i, lake := range lakes {
		label := ""
		if len(lakes) > 1 {
			label = "lake " + lake.Name + ": "
		}
		if lake.Name == config.DefaultLake {
			if err := migrateDefault(env, state, false); err != nil {
				fail(lake, label, err)
				continue
			}
		}
		// A registered lake's key list moves the pin along a rotation,
		// or stops this lake when it cannot be trusted from the pin.
		if uploadPin(lake) != nil {
			next, moved, err := refreshPin(ctx, env, lake)
			var refused *pinRefused
			switch {
			case errors.As(err, &refused):
				fmt.Fprintf(env.stderr(), "terva-lampi: %skeys: %v\n", label, err)
				failed = append(failed, lake.Name)
				last = err
				continue
			case err != nil:
				// The hello below still checks the pin.
				fmt.Fprintf(env.stderr(), "terva-lampi: %skeys: %v\n", label, err)
			case moved:
				fmt.Fprintf(env.stderr(), "terva-lampi: %skey list: pin moved to key %s\n", label, next.KeyID)
				lake = next
			}
		}
		opt, err := lakeOptions(env, file, state, src, lake)
		if err != nil {
			fail(lake, label, err)
			continue
		}
		res, err := syncLake(ctx, opt)
		printSync(env.stdout(), env.stderr(), label, res)
		if err != nil {
			fail(lake, label, err)
		}
		// An interrupt ends the run. The lakes it did not reach are
		// failures, so the exit says the fan-out is incomplete.
		if ctx.Err() != nil {
			for _, rest := range lakes[i+1:] {
				fail(rest, "lake "+rest.Name+": ", fmt.Errorf("not attempted: %w", ctx.Err()))
			}
			break
		}
	}
	switch {
	case len(failed) == 0:
		return nil
	case len(lakes) == 1:
		return last
	default:
		return fmt.Errorf("sync failed for %d of %d lakes: %s", len(failed), len(lakes), strings.Join(failed, ", "))
	}
}

func printSync(stdout, stderr io.Writer, prefix string, res upload.Result) {
	if res.Warning != "" {
		fmt.Fprintf(stderr, "terva-lampi: %s\n", res.Warning)
	}
	for _, s := range res.Skipped {
		fmt.Fprintf(stderr, "terva-lampi: skipped %s\n", s)
	}
	fmt.Fprintf(stdout, "%schecked %d, missing %d, uploaded %d, manifests %d, refused %d, quarantined %d, unchanged %d\n",
		prefix, res.Checked, res.Missing, res.Uploaded, res.Manifests, res.Refused, res.Quarantined, res.Unchanged)
}
