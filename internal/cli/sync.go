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

Walks $TERVA_HOME/sessions, asks the lake which sha256 digests it already
has, PUTs the missing blobs, and POSTs a manifest per session. Unchanged
files upload nothing. A grown file is uploaded whole; tail-only merge is
not implemented.

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
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := upload.Sync(ctx, upload.Options{
		ServerURL: config.ServerURL(file, serverFlag),
		Token:     token,
		TervaHome: home,
		MachineID: m.MachineID,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "checked %d, missing %d, uploaded %d, manifests %d\n",
		res.Checked, res.Missing, res.Uploaded, res.Manifests)
	return nil
}
