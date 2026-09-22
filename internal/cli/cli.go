// Package cli is the terva-lampi command surface.
//
// Dispatch matches terva's shape: each command looks at argv and either
// handles it or declines. There is no third-party flag framework. Streams
// and the environment are passed in so a test can run a command without
// touching the real home directory, the same way git-ticket exposes cli.Run
// for terva to embed.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// version and commit are stamped with -ldflags. 0.0.0 means unstamped.
var (
	version = "0.0.0"
	commit  = ""
)

// Env is the process surface a command is allowed to touch.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Argv0 is the name the binary was invoked as. A base name of `lampi`
	// prints the neurobin collision warning.
	Argv0 string
}

func (e Env) stdin() io.Reader {
	if e.Stdin != nil {
		return e.Stdin
	}
	return os.Stdin
}

func (e Env) stdout() io.Writer {
	if e.Stdout != nil {
		return e.Stdout
	}
	return os.Stdout
}

func (e Env) stderr() io.Writer {
	if e.Stderr != nil {
		return e.Stderr
	}
	return os.Stderr
}

func (e Env) getenv(k string) string {
	if e.Getenv != nil {
		return e.Getenv(k)
	}
	return os.Getenv(k)
}

var errHelp = errors.New("help")

// Run executes one invocation. args is os.Args[1:].
func Run(args []string, env Env) error {
	if msg := aliasWarning(env.Argv0); msg != "" {
		fmt.Fprint(env.stderr(), msg)
	}
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(env.stdout(), rootHelp)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(env.stdout(), versionLine())
		return nil
	}
	var err error
	switch args[0] {
	case "serve":
		err = runServe(env, args[1:])
	case "agent":
		err = runAgent(env, args[1:])
	case "sync":
		err = runSync(env, args[1:])
	case "status":
		err = runStatus(env, args[1:])
	case "login":
		err = runLogin(env, args[1:])
	default:
		fmt.Fprint(env.stdout(), rootHelp)
		return fmt.Errorf("unknown command %q", args[0])
	}
	if errors.Is(err, errHelp) {
		return nil
	}
	return err
}

func versionLine() string {
	if commit == "" {
		return "terva-lampi " + version
	}
	return "terva-lampi " + version + " (" + commit + ")"
}

func isHelp(s string) bool {
	return s == "-h" || s == "--help" || s == "help"
}

func parseFlags(env Env, args []string, usage string, setup func(*flag.FlagSet)) ([]string, error) {
	fs := flag.NewFlagSet("terva-lampi", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	setup(fs)
	if err := fs.Parse(args); err != nil {
		fmt.Fprint(env.stdout(), usage)
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, err
	}
	return fs.Args(), nil
}

func aliasWarning(argv0 string) string {
	base := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	if base != "lampi" {
		return ""
	}
	return "terva-lampi: invoked as `lampi`. The primary command is `terva-lampi`.\n" +
		"terva-lampi: a bare `lampi` on PATH may be neurobin's LAMP installer (https://github.com/neurobin/lampi). Do not replace that binary.\n"
}

const rootHelp = `terva-lampi — a session lake for agent transcripts

lampi is Finnish for a pond. This is that pond: raw session bytes from
the machines you run agents on, content-addressed, in one place.

usage:
  terva-lampi serve     run the lake (health, blob check/put, manifests)
  terva-lampi agent     local capture agent
  terva-lampi sync      push new bytes once
  terva-lampi status    agent state and lake health
  terva-lampi login     write a device token file

The command is terva-lampi. An optional ` + "`lampi`" + ` symlink is not the
primary name. Bare ` + "`lampi`" + ` collides with neurobin's LAMP installer
(https://github.com/neurobin/lampi).

Run ` + "`terva-lampi <command> --help`" + ` for that command's flags.
`
