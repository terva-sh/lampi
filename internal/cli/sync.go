package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

const syncUsage = `terva-lampi sync — push new bytes once

usage:
  terva-lampi sync [--server URL] [--token-file PATH]

Walks terva sessions, Claude Code projects/**/*.jsonl, and Codex
rollout-*.jsonl. history.jsonl is not a Codex rollout. A project is
uploaded only when config.json allowlists it, by cwd prefix, git remote,
or cwd hash. Anything else is refused. Deny rules win. An empty allow
list refuses everything. Claude Code uses $CLAUDE_CONFIG_DIR, or
~/.claude when that is unset. Codex uses $CODEX_HOME, or ~/.codex.

Ruleset v1 scans each file before the lake is contacted. A hit is
quarantined under the state directory and is not uploaded, unless
redaction.upload_hits is set.

Unchanged files upload no new blobs. The watermark moves only after the
lake ACKs the manifest. Pending digests sit in the outbox until that ACK.
A grown file uploads only the new tail. byte_watermark_prev is the
previous length and tail_sha256 is the hash of those bytes. The lake
assembles the tail onto the stored prefix. A clock that disagrees with
hello's server_time by more than five minutes is warned about and the
push still runs.

--server defaults to the URL in config.json, or http://127.0.0.1:8787.
The token is read from a file, never from an argument.
`

func runSync(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), syncUsage)
		return nil
	}
	var serverFlag, tokenFlag string
	rest, err := parseFlags(env, args, syncUsage, func(fs *flag.FlagSet) {
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
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	token, err := resolveToken(env, tokenFlag, file)
	if err != nil {
		return err
	}
	src, err := sources(env.getenv)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := upload.Sync(ctx, upload.Options{
		ServerURL:  config.ServerURL(file, serverFlag),
		Token:      token,
		TervaHome:  homeOf(src, protocol.HarnessTerva),
		ClaudeHome: homeOf(src, protocol.HarnessClaude),
		CodexHome:  homeOf(src, protocol.HarnessCodex),
		MachineID:  m.MachineID,
		StateDir:   state,
		Projects:   file.Projects,
		UploadHits: file.Redaction.UploadHits,
	})
	printSync(env.stdout(), env.stderr(), "", res)
	return err
}

func printSync(stdout, stderr io.Writer, prefix string, res upload.Result) {
	if res.Warning != "" {
		fmt.Fprintf(stderr, "terva-lampi: %s\n", res.Warning)
	}
	fmt.Fprintf(stdout, "%schecked %d, missing %d, uploaded %d, manifests %d, refused %d, quarantined %d\n",
		prefix, res.Checked, res.Missing, res.Uploaded, res.Manifests, res.Refused, res.Quarantined)
}
