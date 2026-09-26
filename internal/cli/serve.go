package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/lakelock"
	"terva.sh/lampi/internal/webconfig"
)

const serveUsage = `terva-lampi serve — run the lake

usage:
  terva-lampi serve [--addr 127.0.0.1:8787] [--data DIR] [--token-file PATH]
                    [--web-config PATH]
  terva-lampi serve backup --out DIR [--data DIR] [--token-file PATH]
                                 copy the catalog, the CAS, and the token file
  terva-lampi serve fsck [--data DIR] [--repair]
                                 re-hash every stored object
  terva-lampi serve purge --session UID [--data DIR] [--yes]
                                 remove one session and its blobs

Listens for capture protocol 1. GET /healthz is open and returns no
catalog data. GET /v1/stats returns session, artifact, and machine
counts. GET /v1/conflicts lists divergent_copy artifacts. Both use the
same auth as the other /v1 routes. /v1/* requires
a device token when --token-file is set. With no token file the
process accepts unauthenticated requests only on a loopback address;
any other --addr is an error. The default bind is 127.0.0.1:8787.

--web-config explicitly enables the OIDC browser interface from a separate
server JSON file. Device tokens are then required even on loopback. The agent's
config.json is not read. The public base_url fixes the callback origin; web
configuration changes require restart. Client secrets are read only from a
private client_secret_file, never command arguments. See deploy/web-config.json.example.
With --token-file, a non-loopback --addr is a stderr warning: serve
speaks plain HTTP, so put TLS in front. Clients refuse to send a
token to a non-loopback http:// URL.

--token-file is a file of device tokens, one per line, or a directory
with one <name>.token file per device. Other files in the directory
are not loaded. A token is 64 lowercase hex characters, as
terva-lampi login writes. A line starting with # is a comment. Any
other line is an error. Each plaintext token is hashed and the file is
rewritten to sha256 lines, comments kept. Copy the device's token
file first; do not point this flag at the device's only copy. The
token is not an argument. SIGHUP reads the token file again. Requests
in flight keep going. A file that does not load leaves the old tokens
in place.

The lake directory holds cas/ (sha256 blobs), catalog.db (SQLite),
normalized/ (one JSONL file per session), and parquet/ (date and
harness partitions). The default is the XDG state dir terva-lampi/,
not $TERVA_HOME. A manifest ACK returns before normalize finishes.

serve holds lake.lock in the lake directory while it runs. A second
serve on the same directory is refused. At start it removes upload
temp files and cas/partial uploads that have not been written for 24h.

SIGTERM stops new requests and waits up to 20s for those in flight,
then drains the normalize queue for up to 30s. Jobs left in the queue
resume at the next start.

Each request writes one line to stderr: method, path, status, bytes,
body bytes, duration, remote address, and X-Forwarded-For when set.
A 200 /healthz is not logged. The Authorization header is not logged.
`

// Shutdown budget. systemd sends SIGKILL 90s after SIGTERM by default;
// the two waits stay under that.
const (
	shutdownGrace  = 20 * time.Second
	normalizeDrain = 30 * time.Second
)

func runServe(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), serveUsage)
		return nil
	}
	if len(args) > 0 {
		switch args[0] {
		case "backup":
			return runServeBackup(env, args[1:])
		case "fsck":
			return runServeFsck(env, args[1:])
		case "purge":
			return runServePurge(env, args[1:])
		}
	}
	var addr, data, tokenFile, webConfigFile string
	rest, err := parseFlags(env, args, serveUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&addr, "addr", "127.0.0.1:8787", "listen address")
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&tokenFile, "token-file", "", "device token file")
		fs.StringVar(&webConfigFile, "web-config", "", "explicit OIDC web configuration file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), serveUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data == "" {
		data, err = config.StateDir(env.getenv)
		if err != nil {
			return err
		}
	}
	var devices *auth.Devices
	if tokenFile != "" {
		devices, err = auth.LoadDevices(tokenFile)
		if err != nil {
			return err
		}
		warnIgnored(env, tokenFile, devices)
	}
	if err := refuseExposedWithoutToken(addr, devices); err != nil {
		return err
	}
	if warn := plaintextTokenWarning(addr, devices); warn != "" {
		fmt.Fprintln(env.stderr(), warn)
	}
	if webConfigFile != "" {
		if _, err := webconfig.Load(webConfigFile); err != nil {
			return err
		}
		if devices == nil || devices.Empty() {
			return fmt.Errorf("web interface requires nonempty device tokens, including on loopback")
		}
	}
	lock, err := lakelock.Acquire(data)
	if err != nil {
		return err
	}
	defer lock.Release()
	lake, err := api.Open(data)
	if err != nil {
		return err
	}
	sweepCAS(env, lake.CAS, time.Now())
	lake.Devices = devices
	lake.Log = accessLogger(env.stderr())

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		lake.Close()
		return err
	}
	fmt.Fprintf(env.stderr(), "terva-lampi serve: listening on %s\n", ln.Addr())
	fmt.Fprintf(env.stderr(), "terva-lampi serve: data %s\n", data)
	if devices == nil || devices.Empty() {
		fmt.Fprintln(env.stderr(), "terva-lampi serve: no device token configured; accepting unauthenticated requests")
	} else if devices.Len() == 1 {
		fmt.Fprintln(env.stderr(), "terva-lampi serve: 1 device token required")
	} else {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: %d device tokens required\n", devices.Len())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if devices != nil {
		reloadOnHangup(ctx, func() { reloadDevices(env, tokenFile, devices) })
	}
	return serveLake(ctx, env, lake, ln, shutdownGrace, normalizeDrain)
}

// reloadDevices reads the token file again and swaps the set in place.
// A handler that has passed the check is not affected. A file that no
// longer loads, or holds no token, keeps the old set: an empty set
// would open a lake that serve refused to expose without one.
func reloadDevices(env Env, path string, devices *auth.Devices) {
	next, err := auth.LoadDevices(path)
	if err != nil {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: token reload failed, keeping %d device tokens: %v\n", devices.Len(), err)
		return
	}
	warnIgnored(env, path, next)
	devices.Replace(next)
	fmt.Fprintf(env.stderr(), "terva-lampi serve: reloaded %d device tokens\n", devices.Len())
}

func warnIgnored(env Env, path string, d *auth.Devices) {
	for _, name := range d.Ignored {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: %s: not loading %s; a device file ends in %s\n", path, name, auth.TokenSuffix)
	}
}

// reloadOnHangup calls reload on each SIGHUP until ctx ends.
func reloadOnHangup(ctx context.Context, reload func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	go func() {
		defer signal.Stop(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				reload()
			}
		}
	}()
}

// sweepAge is how long an upload temp file or a partial upload sits
// untouched before start-up removes it. An agent resumes within it.
const sweepAge = 24 * time.Hour

// sweepCAS runs before the listener opens, so no put is in flight. A
// failure is reported and serve still starts: the leftovers only cost
// disk.
func sweepCAS(env Env, store *cas.Store, now time.Time) {
	n, err := store.Sweep(now.Add(-sweepAge))
	if err != nil {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: sweep: %v\n", err)
	}
	if n > 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: removed %d upload leftovers older than %s\n", n, sweepAge)
	}
}

// newHTTPServer has no ReadTimeout or WriteTimeout. For HTTP/1.1 the
// write deadline starts when the headers are read, so a fixed one drops
// the ACK of a blob whose body is slower than it. The lake handler sets
// read and write deadlines per request, sized from the body.
// ReadHeaderTimeout closes the slowloris gap. These apply on loopback
// and on any address that passed the token check.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// serveLake serves until ctx ends, then shuts down in order: stop
// accepting, wait up to grace for handlers, drain normalize up to
// drain, close the catalog. A handler still running after grace has
// its connection closed; lake.Shutdown still waits for it to return.
func serveLake(ctx context.Context, env Env, lake *api.Server, ln net.Listener, grace, drain time.Duration) error {
	srv := newHTTPServer(lake.Handler())
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	var serveErr error
	select {
	case serveErr = <-served:
	case <-ctx.Done():
		fmt.Fprintln(env.stderr(), "terva-lampi serve: stopping; waiting for requests in flight")
		shut, cancel := context.WithTimeout(context.Background(), grace)
		if err := srv.Shutdown(shut); err != nil {
			fmt.Fprintf(env.stderr(), "terva-lampi serve: requests still open after %s; closing them\n", grace)
			_ = srv.Close()
		}
		cancel()
		serveErr = <-served
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}

	dctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	left, err := lake.Shutdown(dctx)
	if left > 0 {
		fmt.Fprintf(env.stderr(), "terva-lampi serve: %d normalize jobs left after %s; they resume at the next start\n", left, drain)
	}
	return errors.Join(serveErr, err)
}

// accessLogger writes slog text lines without a timestamp. journald
// stamps each line; a terminal does not need two clocks.
func accessLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
}

// refuseExposedWithoutToken rejects a listen that is not loopback when no
// device token is configured. A stderr note is not enough: 0.0.0.0 with an
// empty token set would publish the lake.
func refuseExposedWithoutToken(addr string, devices *auth.Devices) error {
	if devices != nil && !devices.Empty() {
		return nil
	}
	ok, err := listenLoopback(addr)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return fmt.Errorf("refusing %s without a device token; pass --token-file or bind a loopback address", addr)
}

// plaintextTokenWarning is set when device tokens are required on an
// address that is not loopback. serve speaks plain HTTP, so the tokens
// cross that network in the clear unless TLS terminates in front, and
// clients refuse to send a token to a non-loopback http:// URL.
func plaintextTokenWarning(addr string, devices *auth.Devices) string {
	if devices == nil || devices.Empty() {
		return ""
	}
	if ok, err := listenLoopback(addr); err == nil && ok {
		return ""
	}
	return fmt.Sprintf("terva-lampi serve: warning: %s is not loopback and serve speaks plain HTTP; put TLS in front and bind 127.0.0.1, or device tokens cross the network in the clear", addr)
}

// listenLoopback reports whether addr's host is a loopback IP. An empty
// host (":8787") and unspecified addresses (0.0.0.0, ::) are not loopback.
// A hostname is loopback only when every address it resolves to is.
func listenLoopback(addr string) (bool, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false, fmt.Errorf("listen address: %w", err)
	}
	if host == "" {
		return false, nil
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback(), nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, fmt.Errorf("listen address %s: %w", addr, err)
	}
	if len(ips) == 0 {
		return false, nil
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false, nil
		}
	}
	return true, nil
}
