package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
	// syncRetryAfter is the first wait after a failed push. Each further
	// failure doubles the ceiling of a jittered wait, up to syncRetryCap,
	// so a down lake is not hammered and a quiet machine is not stuck
	// until the next append or SIGTERM. A success resets it.
	syncRetryAfter = 2 * time.Second
	syncRetryCap   = 5 * time.Minute
)

// backoff is the wait after a failed push: full jitter between base and
// a ceiling that starts at base and doubles with each failure, up to max.
type backoff struct {
	base, max time.Duration
	fails     int
	// rand returns a value in [0, n).
	rand func(n int64) int64
}

func newBackoff() *backoff {
	return &backoff{base: syncRetryAfter, max: syncRetryCap, rand: rand.Int64N}
}

// next is the wait after one more failure.
func (b *backoff) next() time.Duration {
	ceil := b.max
	if b.fails < 32 && b.base<<b.fails < ceil {
		ceil = b.base << b.fails
	}
	b.fails++
	if ceil <= b.base {
		return ceil
	}
	return b.base + time.Duration(b.rand(int64(ceil-b.base)+1))
}

// capped is the wait after a failure that retrying sooner cannot fix.
func (b *backoff) capped() time.Duration {
	b.fails++
	return b.max
}

func (b *backoff) reset() { b.fails = 0 }

// agentBackoff is replaced in tests.
var agentBackoff = newBackoff

func tokenRefused(path, token string, err error, wait time.Duration) string {
	var se *upload.StatusError
	status := "refused"
	if errors.As(err, &se) {
		status = se.Status
	}
	if token == "" {
		return fmt.Sprintf("lake answered %s and no device token was sent; %s does not exist. Run terva-lampi login or pass --token-file. Retrying every %s", status, path, wait)
	}
	return fmt.Sprintf("lake answered %s to the device token in %s. Check that serve --token-file lists it. Retrying every %s", status, path, wait)
}

const agentUsage = `terva-lampi agent — local capture

usage:
  terva-lampi agent [--server URL] [--token-file PATH]
                                 watch session JSONL and upload until signalled
  terva-lampi agent discover     list session JSONL files
  terva-lampi agent machine-id   print the stable machine id, creating it if needed
  terva-lampi agent config       print paths, the effective server URL and
                                 token file, and which layer set each
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
database has an empty cwd and is refused by design. A workspace
export copies that global database read-only and merges cursorDiskKV
rows for composers named by that workspace's composer.composerHeaders.
The session id stays workspace/<id>. A workspace database takes its
cwd from workspace.json. The Cursor CLI store
is a separate corpus. On Linux the config directory is
$XDG_CONFIG_HOME/cursor when that variable is set, otherwise
~/.cursor. CURSOR_CONFIG_DIR replaces it. On macOS it is ~/.cursor.
On Windows it is the .cursor directory under USERPROFILE. Each chat is
chats/<workspace>/<session>/store.db. The upload is a JSON export
of a snapshot. The IDE reader does not open it, and this reader
does not open state.vscdb. Keys under cursorAuth/ and credential
fields such as accessToken are not in the export. The raw database
and its -wal and -shm files are not uploaded. A chat needs an
absolute cwd in the sibling meta.json. A relative path or a file URI
is an empty cwd, so the allowlist refuses it. The workspace directory
name is not a path. A refused cursor or cursor-cli session with an
empty cwd is named on stderr with that reason. config.json harnesses
can turn a harness off or point it at
another directory. The id is terva, claude, codex, opencode, cursor,
or cursor-cli. enabled false skips discover, watch, and upload for
that id. The watermark and any object already stored stay. root is
an absolute path and wins over the environment variable and the
default above. There is no per-harness root flag. Omit harnesses,
or omit one id, and that harness stays on and keeps the resolution
above. A home that does not exist yet, terva included, is polled
until it appears. A file that cannot be read, or whose session id
would sit on a line past the reader's cap, is skipped and named on
stderr; the other files upload. The watcher prefers fsnotify. It
polls instead when fsnotify cannot add a directory, such as at the
inotify watch limit, or reports an error such as a queue overflow,
and it names the path on stderr. On macOS it polls by default,
since kqueue holds a descriptor per file. LAMPI_WATCH=poll or
LAMPI_WATCH=fsnotify overrides that on any platform.

The machine id is the one in the config directory. Growth, and one pass
at startup for files already on disk, call the same path as
terva-lampi sync: hello, allowlist, ruleset v2, watermark, outbox, then
the upload. A failed push is logged and tried again, even when the file
does not grow. The first wait is 2s. Each further failure doubles the
ceiling of a jittered wait, up to 5 minutes, and a success resets it.
Growth during that wait does not start a push. A 401 or 403 is logged
once, naming the token file, and waits the full 5 minutes. A project
the allowlist or the scan refused is not retried. SIGTERM or interrupt
drains the outbox best-effort and exits. On Unix, SIGUSR1 asks for a
sync now. The filesystem watch is still the source of truth; the
signal only skips the wait. While the
daemon runs it writes agent.pid in the state directory and holds a
lock on it. A second agent on that directory exits and names the
first one's pid.
hooks/terva-post-tool-enqueue.sh can send that signal from a terva
post_tool_use hook. It is optional, and make build does not install
it.

--server defaults to LAMPI_SERVER, then the URL in config.json, or
http://127.0.0.1:8787. --token-file defaults to LAMPI_TOKEN_FILE, then
the token path in config.json, then the token file in the config
directory. sync, status, and agent config resolve both the same way,
and status and agent config print source=flag, env, config, or
default for each. The token is read from a file, never from an
argument. The agent does not start when a token would go to an
http:// URL whose host is not localhost, 127.0.0.0/8, or ::1. Use
https for a remote lake. The server URL, token, allowlist, and
harnesses map are read at start; restart the process to reload them.
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
// falls through to LAMPI_SERVER, LAMPI_TOKEN_FILE, then config.json,
// the same order sync and status use.
func runAgentLoop(ctx context.Context, env Env, serverFlag, tokenFlag string) error {
	opt, tokenPath, src, n, err := loadAgent(env, serverFlag, tokenFlag)
	if err != nil {
		return err
	}
	poll, err := watch.PollByDefault(env.getenv)
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
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Backend(poll))

	kick := make(chan struct{}, 1)
	wake := func() {
		select {
		case kick <- struct{}{}:
		default:
		}
	}
	// waiting is set while a failed push waits to retry. Growth then does
	// not start a push: the retry picks it up, and the lake is not asked
	// again on every append. SIGUSR1 still asks for one now.
	var waiting atomic.Bool
	watchKick(ctx, func() {
		waiting.Store(false)
		wake()
	})
	watchers := startWatches(src, poll, func(c watch.Change) {
		fmt.Fprintf(env.stdout(), "watch: %s %s offset=%d size=%d\n", c.Op, c.RelPath, c.Offset, c.Size)
		if !waiting.Load() {
			wake()
		}
	}, func(err error) {
		fmt.Fprintf(env.stderr(), "terva-lampi: %v\n", err)
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
		waiting.Store(false)
		if retry != nil {
			retry.Stop()
			retry = nil
		}
	}
	armRetry := func(d time.Duration) {
		retryMu.Lock()
		defer retryMu.Unlock()
		if retry != nil {
			retry.Stop()
		}
		waiting.Store(true)
		retry = time.AfterFunc(d, func() {
			waiting.Store(false)
			wake()
		})
	}
	defer disarmRetry()
	bo := agentBackoff()
	// authLogged holds the 401 line to one per run of refusals. A success
	// or a different error lets it print again.
	authLogged := false

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
			// A token the lake refuses will not start working in two
			// seconds. Say so once, naming the file, and wait the cap.
			if upload.Unauthorized(err) {
				if !authLogged {
					authLogged = true
					fmt.Fprintf(env.stderr(), "terva-lampi: %s\n", tokenRefused(tokenPath, opt.Token, err, bo.max))
				}
				armRetry(bo.capped())
				continue
			}
			authLogged = false
			if err != nil {
				fmt.Fprintf(env.stderr(), "terva-lampi: %v\n", err)
				// A refusal is the allowlist or the scan. It will not
				// change until the process is restarted with a new config.
				// The rest of the pass reached the lake.
				if _, refused := err.(*upload.Rejected); refused {
					bo.reset()
					disarmRetry()
					continue
				}
				armRetry(bo.next())
				continue
			}
			bo.reset()
			disarmRetry()
		}
	}
}

// loadAgent resolves the options once at start. tokenPath is the file
// the token came from, or would have, for the 401 log line.
func loadAgent(env Env, serverFlag, tokenFlag string) (opt upload.Options, tokenPath string, src []source, n int, err error) {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	tokenFile, err := tokenPathFor(env, tokenFlag, file)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	tokenPath = tokenFile.Value
	token, err := resolveToken(env, tokenFlag, file)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	server := config.ResolveServer(file, env.getenv, serverFlag).Value
	// A token that would cross the network in the clear stops the agent
	// at start. It cannot change until the config does.
	if err := upload.CheckToken(server, token); err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	src, n, err = countSources(env.getenv, file.Harnesses)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return upload.Options{}, "", nil, 0, err
	}
	return upload.Options{
		ServerURL:     server,
		Token:         token,
		PieceBytes:    upload.DefaultPieceBytes,
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
	}, tokenPath, src, n, nil
}

func countSources(getenv func(string) string, harnesses config.Harnesses) ([]source, int, error) {
	src, err := sources(getenv, harnesses)
	if err != nil {
		return nil, 0, err
	}
	files, err := discoverSources(context.Background(), src)
	if err != nil {
		return nil, 0, err
	}
	return src, len(files), nil
}

// startWatches builds one watcher per harness tree. A home that does
// not exist yet is still watched: the watcher polls until it appears,
// so a harness installed after the agent started is picked up. poll
// forces the poll backend. onFallback hears a watcher that moved from
// fsnotify to polling.
func startWatches(src []source, poll bool, on func(watch.Change), onFallback func(error)) []*watch.Watcher {
	var out []*watch.Watcher
	for _, s := range src {
		for _, dir := range s.watchDirs() {
			layout := s.layout()
			layout.Dir = dir
			out = append(out, &watch.Watcher{
				Root:       s.home,
				Layout:     layout,
				OnChange:   on,
				ForcePoll:  poll,
				OnFallback: onFallback,
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
	tokenFile, err := tokenPathFor(env, "", file)
	if err != nil {
		return err
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
	writeEndpoint(env.stdout(), config.ResolveServer(file, env.getenv, ""), tokenFile)
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
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	m, err := config.LoadMachine(env.getenv)
	if err != nil {
		return err
	}
	src, n, err := countSources(env.getenv, file.Harnesses)
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
	poll, err := watch.PollByDefault(env.getenv)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "watch: %s\n", watch.Backend(poll))
	return writeCaptureState(env.stdout(), state)
}

// writeAgentPID records this process in the state directory so a hook
// can ask for a sync. It also keeps a second agent on the same state
// directory from starting. The file is removed when the daemon returns.
func writeAgentPID(stateDir string) (func(), error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("agent: pid file: %w", err)
	}
	return lockAgentPID(filepath.Join(stateDir, "agent.pid"))
}

// errAgentRunning is the refusal a second agent prints. The pid is
// read from the file for the message only.
func errAgentRunning(path string) error {
	raw, _ := os.ReadFile(path)
	pid := strings.TrimSpace(string(raw))
	if pid == "" {
		pid = "unknown"
	}
	return fmt.Errorf("agent: another terva-lampi agent is running (pid %s, %s); stop it first", pid, path)
}
