package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
	"terva.sh/lampi/internal/watch"
)

const (
	// drainTimeout bounds the shutdown upload. The signal that stopped the
	// watch must not also give up on rows already sitting in the outbox, and
	// it must not wait forever on a lake that is not answering.
	drainTimeout = 30 * time.Second
	// syncRetryAfter is how long a failed push waits before trying again.
	// File growth still syncs immediately. Each new failure resets the wait
	// so a down lake is not hammered, and a quiet machine is not stuck
	// until the next append or SIGTERM.
	syncRetryAfter = 2 * time.Second
)

const agentUsage = `terva-lampi agent — local capture

usage:
  terva-lampi agent [--server URL] [--token-file PATH]
                                 watch session JSONL and upload until signalled
  terva-lampi agent discover     list session JSONL files
  terva-lampi agent machine-id   print the stable machine id, creating it if needed
  terva-lampi agent config       print paths and the effective server URL
  terva-lampi agent status       local identity, outbox, watermarks, and last sync

terva sessions are read from TERVA_HOME, then ZOT_HOME, then the platform
default terva uses. Optional sidecars in that home are
raati/raati-<nanos>.json and tasks/tasks-<session-id>.json, plus the
legacy ext-data/tasks tree. A missing directory is skipped. A sidecar
uploads only with a session the allowlist already permits. Claude Code
sessions are $CLAUDE_CONFIG_DIR/projects/**/*.jsonl,
or ~/.claude/projects when that variable is unset. Codex rollouts are
$CODEX_HOME/sessions/**/rollout-*.jsonl, or ~/.codex/sessions when that
variable is unset. history.jsonl is not a Codex rollout. OpenCode is
a scheduled opencode export under the data directory's export/. That
directory is $XDG_DATA_HOME/opencode, or ~/.local/share/opencode when
that variable is unset. When export/ has no JSON, the database file at
the data-directory root is the fallback. opencode.db-wal and
opencode.db-shm are not read. Cursor IDE state is a read-only snapshot
of state.vscdb under the user-data directory. On Linux that directory
is $XDG_CONFIG_HOME/Cursor, or ~/.config/Cursor. On macOS it is
~/Library/Application Support/Cursor. On Windows it is %APPDATA%\Cursor.
Global storage and each workspaceStorage directory are separate. The
upload is a JSON export. Keys under cursorAuth/ are not in it. The raw
database and its -wal and -shm files are not uploaded. The global
database has no single project, so the allowlist refuses it. A
workspace takes its cwd from workspace.json. The Cursor CLI store
is a separate corpus. On Linux the config directory is
$XDG_CONFIG_HOME/cursor when that variable is set, otherwise
~/.cursor. CURSOR_CONFIG_DIR replaces it. On macOS it is ~/.cursor.
On Windows it is the .cursor directory under USERPROFILE. Each chat is
chats/<workspace>/<session>/store.db. The upload is a JSON export
of a snapshot. The IDE reader does not open it, and this reader
does not open state.vscdb. Keys under cursorAuth/ and credential
fields such as accessToken are not in the export. The raw database
and its -wal and -shm files are not uploaded. A chat whose meta.json
has an absolute cwd uses that path. Anything else has an empty cwd,
so the allowlist refuses it. The workspace directory name is not a
path. config.json harnesses can turn a harness off or point it at
another directory. The id is terva, claude, codex, opencode, cursor,
or cursor-cli. enabled false skips discover, watch, and upload for
that id. The watermark and any object already stored stay. root is
an absolute path and wins over the environment variable and the
default above. There is no per-harness root flag. Omit harnesses,
or omit one id, and that harness stays on and keeps the resolution
above. A missing Claude, Codex, OpenCode, or Cursor
directory is skipped. The watcher prefers fsnotify and falls back
to polling.

The machine id is the one in the config directory. Growth, and one pass
at startup for files already on disk, call the same path as
terva-lampi sync: allowlist, ruleset v1, watermark, outbox, then the
lake. A failed push is logged and tried again after a short wait, even
when the file does not grow. A project the allowlist or the scan refused
is not retried. SIGTERM or interrupt drains the outbox best-effort and
exits. On Unix, SIGUSR1 asks for a sync now. The filesystem watch is
still the source of truth; the signal only skips the wait. While the
daemon runs it writes agent.pid in the state directory.
hooks/terva-post-tool-enqueue.sh can send that signal from a terva
post_tool_use hook. It is optional, and make build does not install
it.

--server defaults to LAMPI_SERVER, then the URL in config.json, or
http://127.0.0.1:8787. --token-file defaults to LAMPI_TOKEN_FILE, then
the token path in config.json. The token is read from a file, never
from an argument. The server URL, token, allowlist, and harnesses map are read at
start; restart the process to reload them.
`

func runAgent(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), agentUsage)
		return nil
	}
	sub := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "":
		return runAgentDaemon(env, args)
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

func runAgentDaemon(env Env, args []string) error {
	var serverFlag, tokenFlag string
	rest, err := parseFlags(env, args, agentUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&serverFlag, "server", "", "lake base URL")
		fs.StringVar(&tokenFlag, "token-file", "", "device token file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), agentUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runAgentLoop(ctx, env, serverFlag, tokenFlag)
}

// runAgentLoop is the long-running process. ctx ending is shutdown:
// the watch stops, then the outbox is drained on a new context.
// serverFlag and tokenFlag are the command-line overrides. Empty
// falls through to LAMPI_SERVER, LAMPI_TOKEN_FILE, then config.json.
func runAgentLoop(ctx context.Context, env Env, serverFlag, tokenFlag string) error {
	opt, src, n, err := loadAgent(env, serverFlag, tokenFlag)
	if err != nil {
		return err
	}
	removePID, err := writeAgentPID(opt.StateDir)
	if err != nil {
		return err
	}
	defer removePID()
	// Banner and watch lines share a mutex so a change report and a
	// sync summary do not split each other on the way out.
	var mu sync.Mutex
	env.Stdout = &syncWriter{mu: &mu, w: env.stdout()}
	env.Stderr = &syncWriter{mu: &mu, w: env.stderr()}

	fmt.Fprintf(env.stdout(), "machine_id: %s\n", opt.MachineID)
	for _, s := range src {
		fmt.Fprintf(env.stdout(), "%s: %s\n", homeLabel(s.harness.Name()), s.home)
	}
	fmt.Fprintf(env.stdout(), "sessions: %d\n", n)
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Probe())

	kick := make(chan struct{}, 1)
	wake := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	watchKick(ctx, wake)
	watchers := startWatches(src, func(c watch.Change) {
		fmt.Fprintf(env.stdout(), "watch: %s %s offset=%d size=%d\n", c.Op, c.RelPath, c.Offset, c.Size)
		wake()
	})
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	watchErr := make(chan error, len(watchers))
	for _, w := range watchers {
		go func(w *watch.Watcher) { watchErr <- w.Run(watchCtx) }(w)
	}
	// watchErr is read in exactly one place per shutdown. WaitReady
	// does not read it: a failed Run never closes the ready channel,
	// and a nil return from Run is a clean stop, not a second result.
	go func() {
		for _, w := range watchers {
			if err := w.WaitReady(watchCtx); err != nil {
				return
			}
		}
		fmt.Fprintln(env.stdout(), "watching")
		// Files already on disk are not a watch event. One sync at
		// start is how they reach the lake before the next append.
		wake()
	}()

	// One timer, reset on each failure. The callback only wakes the
	// loop, so a retry cannot run beside the sync already in progress.
	var retryMu sync.Mutex
	var retry *time.Timer
	disarmRetry := func() {
		retryMu.Lock()
		defer retryMu.Unlock()
		if retry != nil {
			retry.Stop()
			retry = nil
		}
	}
	armRetry := func() {
		retryMu.Lock()
		defer retryMu.Unlock()
		if retry != nil {
			retry.Stop()
		}
		retry = time.AfterFunc(syncRetryAfter, wake)
	}
	defer disarmRetry()

	for {
		select {
		case <-ctx.Done():
			disarmRetry()
			watchCancel()
			_ = waitWatches(watchErr, len(watchers))
			return drainAgent(env, opt)
		case err := <-watchErr:
			disarmRetry()
			watchCancel()
			if rest := waitWatches(watchErr, len(watchers)-1); err == nil {
				err = rest
			}
			drainAgent(env, opt)
			if ctx.Err() != nil {
				return nil
			}
			return err
		case <-kick:
			// A kick that was already queued must not start a sync once
			// shutdown has begun. The drain below is the last push.
			var err error
			if ctx.Err() == nil {
				err = runAgentSync(ctx, env, opt, "")
			}
			if ctx.Err() != nil {
				disarmRetry()
				watchCancel()
				_ = waitWatches(watchErr, len(watchers))
				return drainAgent(env, opt)
			}
			if err != nil {
				fmt.Fprintf(env.stderr(), "terva-lampi: %v\n", err)
				// A refusal is the allowlist or the scan. It will not
				// change until the process is restarted with a new config.
				if _, refused := err.(*upload.Rejected); refused {
					disarmRetry()
					continue
				}
				armRetry()
				continue
			}
			disarmRetry()
		}
	}
}

func loadAgent(env Env, serverFlag, tokenFlag string) (upload.Options, []source, int, error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return upload.Options{}, nil, 0, err
	}
	if serverFlag == "" {
		serverFlag = env.getenv("LAMPI_SERVER")
	}
	if tokenFlag == "" {
		tokenFlag = env.getenv("LAMPI_TOKEN_FILE")
	}
	token, err := resolveToken(env, tokenFlag, file)
	if err != nil {
		return upload.Options{}, nil, 0, err
	}
	src, n, err := countSources(env)
	if err != nil {
		return upload.Options{}, nil, 0, err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return upload.Options{}, nil, 0, err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return upload.Options{}, nil, 0, err
	}
	return upload.Options{
		ServerURL:     config.ServerURL(file, serverFlag),
		Token:         token,
		TervaHome:     homeOf(src, protocol.HarnessTerva),
		ClaudeHome:    homeOf(src, protocol.HarnessClaude),
		CodexHome:     homeOf(src, protocol.HarnessCodex),
		OpenCodeHome:  homeOf(src, protocol.HarnessOpenCode),
		CursorHome:    homeOf(src, protocol.HarnessCursor),
		CursorCLIHome: homeOf(src, protocol.HarnessCursorCLI),
		MachineID:     m.MachineID,
		StateDir:      state,
		Projects:      file.Projects,
		UploadHits:    file.Redaction.UploadHits,
	}, src, n, nil
}

func countSources(env Env) ([]source, int, error) {
	src, err := configuredSources(env.getenv)
	if err != nil {
		return nil, 0, err
	}
	files, err := discoverSources(context.Background(), src)
	if err != nil {
		return nil, 0, err
	}
	return src, len(files), nil
}

func startWatches(src []source, on func(watch.Change)) []*watch.Watcher {
	var out []*watch.Watcher
	for _, s := range src {
		if !s.watch() {
			continue
		}
		for _, dir := range s.watchDirs() {
			layout := s.layout()
			layout.Dir = dir
			out = append(out, &watch.Watcher{
				Root:     s.home,
				Layout:   layout,
				OnChange: on,
			})
		}
	}
	return out
}

// waitWatches reads n results. The caller has already cancelled the
// watch context. A nil slice of watchers reads nothing.
func waitWatches(watchErr <-chan error, n int) error {
	var first error
	for i := 0; i < n; i++ {
		if err := <-watchErr; err != nil && first == nil {
			first = err
		}
	}
	return first
}

func runAgentSync(ctx context.Context, env Env, opt upload.Options, prefix string) error {
	res, err := upload.Sync(ctx, opt)
	printSync(env.stdout(), env.stderr(), prefix, res)
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
	src, err := configuredSources(env.getenv)
	if err != nil {
		return err
	}
	files, err := discoverSources(context.Background(), src)
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Fprintf(env.stdout(), "%s\t%s\n", f.harness, f.ref.RelPath)
	}
	if len(files) == 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi: no session files\n")
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
	src, err := sources(env.getenv, file.Harnesses)
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
	for _, s := range src {
		fmt.Fprintf(env.stdout(), "%s: %s\n", homeLabel(s.harness.Name()), s.home)
	}
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
	src, n, err := countSources(env)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	id := m.MachineID
	if id == "" {
		id = "(not created)"
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", id)
	for _, s := range src {
		fmt.Fprintf(env.stdout(), "%s: %s\n", homeLabel(s.harness.Name()), s.home)
	}
	fmt.Fprintf(env.stdout(), "sessions: %d\n", n)
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Probe())
	return writeCaptureState(env.stdout(), state)
}

// writeAgentPID records this process in the state directory so a hook
// can ask for a sync. The file is removed when the daemon returns.
func writeAgentPID(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("agent: pid file: %w", err)
	}
	path := filepath.Join(stateDir, "agent.pid")
	body := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return nil, fmt.Errorf("agent: pid file: %w", err)
	}
	return func() { _ = os.Remove(path) }, nil
}
