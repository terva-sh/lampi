package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/auth"
	"terva.sh/lampi/internal/config"
)

const serveUsage = `terva-lampi serve — run the lake

usage:
  terva-lampi serve [--addr 127.0.0.1:8787] [--data DIR] [--token-file PATH]

Listens for capture protocol 1. GET /healthz is open and returns no
catalog data. GET /v1/stats returns session, artifact, and machine
counts and uses the same auth as the other /v1 routes. /v1/* requires
the device token when --token-file is set. With no token file the
process accepts unauthenticated requests only on a loopback address;
any other --addr is an error. The default bind is 127.0.0.1:8787.

The lake directory holds cas/ (sha256 blobs) and catalog.db (SQLite).
The default is the XDG state dir terva-lampi/, not $TERVA_HOME.
`

func runServe(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), serveUsage)
		return nil
	}
	var addr, data, tokenFile string
	rest, err := parseFlags(env, args, serveUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&addr, "addr", "127.0.0.1:8787", "listen address")
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&tokenFile, "token-file", "", "device token file")
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
	var token string
	if tokenFile != "" {
		token, err = auth.Read(tokenFile)
		if err != nil {
			return err
		}
	}
	if err := refuseExposedWithoutToken(addr, token); err != nil {
		return err
	}
	lake, err := api.Open(data)
	if err != nil {
		return err
	}
	defer lake.Close()
	lake.Token = token

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stderr(), "terva-lampi serve: listening on %s\n", ln.Addr())
	fmt.Fprintf(env.stderr(), "terva-lampi serve: data %s\n", data)
	if token == "" {
		fmt.Fprintln(env.stderr(), "terva-lampi serve: no device token configured; accepting unauthenticated requests")
	} else {
		fmt.Fprintln(env.stderr(), "terva-lampi serve: device token required")
	}

	srv := &http.Server{
		Handler: lake.Handler(),
		// ReadHeaderTimeout closes the slowloris gap. ReadTimeout covers the
		// body and is long enough for the 32 MiB blob cap. Both apply on
		// loopback and on any address that passed the token check.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// refuseExposedWithoutToken rejects a listen that is not loopback when no
// device token is configured. A stderr note is not enough: 0.0.0.0 with an
// empty token would publish the lake.
func refuseExposedWithoutToken(addr, token string) error {
	if token != "" {
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
