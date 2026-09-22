package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/upload"
)

const syncUsage = `terva-lampi sync — push new bytes once

usage:
  terva-lampi sync [--server URL] [--token-file PATH]

Walks $TERVA_HOME/sessions. A project is uploaded only when config.json
allowlists it, by cwd prefix, git remote, or terva cwd hash. Anything
else is refused. Deny rules win. An empty allow list refuses everything.

Ruleset v1 scans each file before the lake is contacted. A hit is
quarantined under the state directory and is not uploaded, unless
redaction.upload_hits is set.

Unchanged files upload no new blobs. The watermark moves only after the
lake ACKs the manifest. Pending digests sit in the outbox until that ACK.
A grown file is still uploaded whole; the lake does not assemble tails yet.

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
	home, err := discover.TervaHome(env.getenv)
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
		TervaHome:  home,
		MachineID:  m.MachineID,
		StateDir:   state,
		Projects:   file.Projects,
		UploadHits: file.Redaction.UploadHits,
	})
	fmt.Fprintf(env.stdout(), "checked %d, missing %d, uploaded %d, manifests %d, refused %d, quarantined %d\n",
		res.Checked, res.Missing, res.Uploaded, res.Manifests, res.Refused, res.Quarantined)
	return err
}
