package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/watch"
)

const agentUsage = `terva-lampi agent — local capture

usage:
  terva-lampi agent              discover, then wait (filesystem watch is not implemented)
  terva-lampi agent discover     list $TERVA_HOME/sessions JSONL files
  terva-lampi agent machine-id   print the stable machine id, creating it if needed
  terva-lampi agent config       print paths and the effective server URL
  terva-lampi agent status       local identity and how many session files are visible

Sessions are read from TERVA_HOME, then ZOT_HOME, then the platform default
terva uses. The watcher that will tail those files is a stub: it waits
until the process is signalled. Use terva-lampi sync to push.
`

func runAgent(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), agentUsage)
		return nil
	}
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "":
		return runAgentDaemon(env)
	case "discover":
		return runAgentDiscover(env)
	case "machine-id":
		m, err := config.EnsureMachine(env.getenv)
		if err != nil {
			return err
		}
		fmt.Fprintln(env.stdout(), m.MachineID)
		return nil
	case "config":
		return runAgentConfig(env)
	case "status":
		return runAgentStatus(env)
	default:
		fmt.Fprint(env.stdout(), agentUsage)
		return fmt.Errorf("unknown agent command %q", sub)
	}
}

func runAgentDaemon(env Env) error {
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return err
	}
	home, files, err := sessionFiles(env)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", m.MachineID)
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", home)
	fmt.Fprintf(env.stdout(), "sessions: %d\n", len(files))
	fmt.Fprintln(env.stdout(), "filesystem watch is not implemented; waiting. Use `terva-lampi sync` to push.")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return watch.Unimplemented{}.Run(ctx)
}

func runAgentDiscover(env Env) error {
	home, files, err := sessionFiles(env)
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Fprintln(env.stdout(), f.RelPath)
	}
	if len(files) == 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi: no session files under %s\n", home)
	}
	return nil
}

func runAgentConfig(env Env) error {
	cfgDir, err := config.ConfigDir(env.getenv)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	tokenPath, err := config.TokenPath(env.getenv)
	if err != nil {
		return err
	}
	if file.TokenFile != "" {
		tokenPath = file.TokenFile
	}
	home, err := discover.TervaHome(env.getenv)
	if err != nil {
		return err
	}
	m, err := config.LoadMachine(env.getenv)
	if err != nil {
		return err
	}
	machine := m.MachineID
	if machine == "" {
		machine = "(not created; run terva-lampi agent machine-id)"
	}
	fmt.Fprintf(env.stdout(), "config_dir: %s\n", cfgDir)
	fmt.Fprintf(env.stdout(), "state_dir: %s\n", state)
	fmt.Fprintf(env.stdout(), "server: %s\n", config.ServerURL(file, ""))
	fmt.Fprintf(env.stdout(), "token_file: %s\n", tokenPath)
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", home)
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", machine)
	return nil
}

func runAgentStatus(env Env) error {
	m, err := config.LoadMachine(env.getenv)
	if err != nil {
		return err
	}
	home, files, err := sessionFiles(env)
	if err != nil {
		return err
	}
	id := m.MachineID
	if id == "" {
		id = "(not created)"
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", id)
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", home)
	fmt.Fprintf(env.stdout(), "sessions: %d\n", len(files))
	fmt.Fprintln(env.stdout(), "watch: not implemented")
	return nil
}

func sessionFiles(env Env) (string, []discover.File, error) {
	home, err := discover.TervaHome(env.getenv)
	if err != nil {
		return "", nil, err
	}
	files, err := discover.Sessions(home)
	if err != nil {
		return "", nil, err
	}
	return home, files, nil
}
