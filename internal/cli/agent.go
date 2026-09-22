package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/upload"
	"terva.sh/lampi/internal/watch"
)

// drainTimeout bounds the shutdown upload. The signal that stopped the
// watch must not also give up on rows already sitting in the outbox, and
// it must not wait forever on a lake that is not answering.
const drainTimeout = 30 * time.Second

const agentUsage = `terva-lampi agent — local capture

usage:
  terva-lampi agent              watch $TERVA_HOME/sessions and upload until signalled
  terva-lampi agent discover     list $TERVA_HOME/sessions JSONL files
  terva-lampi agent machine-id   print the stable machine id, creating it if needed
  terva-lampi agent config       print paths and the effective server URL
  terva-lampi agent status       local identity and how many session files are visible

Sessions are read from TERVA_HOME, then ZOT_HOME, then the platform default
terva uses. The watcher prefers fsnotify and falls back to polling.

The machine id is the one in the config directory. Growth, and one pass
at startup for files already on disk, call the same path as
terva-lampi sync: allowlist, ruleset v1, watermark, outbox, then the
lake. A failed push is logged. The next change tries again. SIGTERM or
interrupt drains the outbox best-effort and exits.
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runAgentLoop(ctx, env)
}

// runAgentLoop is the long-running process. ctx ending is shutdown:
// the watch stops, then the outbox is drained on a new context.
func runAgentLoop(ctx context.Context, env Env) error {
	opt, n, err := loadAgent(env)
	if err != nil {
		return err
	}
	// Banner and watch lines share a mutex so a change report and a
	// sync summary do not split each other on the way out.
	var mu sync.Mutex
	env.Stdout = &syncWriter{mu: &mu, w: env.stdout()}
	env.Stderr = &syncWriter{mu: &mu, w: env.stderr()}

	fmt.Fprintf(env.stdout(), "machine_id: %s\n", opt.MachineID)
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", opt.TervaHome)
	fmt.Fprintf(env.stdout(), "sessions: %d\n", n)
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Probe())

	kick := make(chan struct{}, 1)
	wake := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	w := &watch.Watcher{
		Root: opt.TervaHome,
		OnChange: func(c watch.Change) {
			fmt.Fprintf(env.stdout(), "watch: %s %s offset=%d size=%d\n", c.Op, c.RelPath, c.Offset, c.Size)
			wake()
		},
	}
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	watchErr := make(chan error, 1)
	go func() { watchErr <- w.Run(watchCtx) }()

	if err := waitWatch(watchCtx, w, watchErr); err != nil {
		if ctx.Err() != nil {
			return drainAgent(env, opt)
		}
		drainAgent(env, opt)
		return err
	}
	fmt.Fprintln(env.stdout(), "watching")
	// Files already on disk are not a watch event. One sync at start
	// is how they reach the lake before the next append.
	wake()

	for {
		select {
		case <-ctx.Done():
			watchCancel()
			<-watchErr
			return drainAgent(env, opt)
		case err := <-watchErr:
			drainAgent(env, opt)
			return err
		case <-kick:
			err := runAgentSync(ctx, env, opt, "")
			if ctx.Err() != nil {
				watchCancel()
				<-watchErr
				return drainAgent(env, opt)
			}
			if err != nil {
				fmt.Fprintf(env.stderr(), "terva-lampi: %v\n", err)
			}
		}
	}
}

func loadAgent(env Env) (upload.Options, int, error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return upload.Options{}, 0, err
	}
	token, err := resolveToken(env, "", file)
	if err != nil {
		return upload.Options{}, 0, err
	}
	home, files, err := sessionFiles(env)
	if err != nil {
		return upload.Options{}, 0, err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return upload.Options{}, 0, err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return upload.Options{}, 0, err
	}
	return upload.Options{
		ServerURL:  config.ServerURL(file, ""),
		Token:      token,
		TervaHome:  home,
		MachineID:  m.MachineID,
		StateDir:   state,
		Projects:   file.Projects,
		UploadHits: file.Redaction.UploadHits,
	}, len(files), nil
}

// waitWatch blocks until the first walk has seeded cursors, the watcher
// has failed, or ctx is done. A failed Run does not close the ready
// channel, so this selects on both.
func waitWatch(ctx context.Context, w *watch.Watcher, watchErr <-chan error) error {
	ready := make(chan error, 1)
	go func() { ready <- w.WaitReady(ctx) }()
	select {
	case err := <-watchErr:
		return err
	case err := <-ready:
		return err
	}
}

func runAgentSync(ctx context.Context, env Env, opt upload.Options, prefix string) error {
	res, err := upload.Sync(ctx, opt)
	fmt.Fprintf(env.stdout(), "%schecked %d, missing %d, uploaded %d, manifests %d, refused %d, quarantined %d\n",
		prefix, res.Checked, res.Missing, res.Uploaded, res.Manifests, res.Refused, res.Quarantined)
	return err
}

// drainAgent pushes whatever the outbox still holds. The context is new
// on purpose: the one that stopped the watch is already cancelled, and
// using it would abort the drain it exists to finish. An error is logged
// and not returned. Shutdown still succeeds.
func drainAgent(env Env, opt upload.Options) error {
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	fmt.Fprintln(env.stderr(), "terva-lampi: draining outbox")
	if err := runAgentSync(ctx, env, opt, "drain: "); err != nil {
		fmt.Fprintf(env.stderr(), "terva-lampi: drain: %v\n", err)
	}
	return nil
}

type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
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
	fmt.Fprintf(env.stdout(), "projects_allow: %d\n", len(file.Projects.Allow))
	fmt.Fprintf(env.stdout(), "projects_deny: %d\n", len(file.Projects.Deny))
	fmt.Fprintf(env.stdout(), "redaction_upload_hits: %t\n", file.Redaction.UploadHits)
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
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Probe())
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
