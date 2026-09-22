// Command terva-lampi is the session lake: one static binary with the
// server (serve) and the per-machine client (agent, sync, status, login).
package main

import (
	"fmt"
	"os"

	"terva.sh/lampi/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], cli.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		Argv0:  os.Args[0],
	}); err != nil {
		fmt.Fprintln(os.Stderr, "terva-lampi:", err)
		os.Exit(1)
	}
}
