package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const healthcheckUsage = `terva-lampi serve healthcheck — probe a running lake

usage:
  terva-lampi serve healthcheck [--addr 127.0.0.1:8787] [--timeout 3s]

Sends GET /healthz to the lake listening on --addr and exits 0 when it
answers 200 {"status":"ok"}. Anything else, including no answer within
--timeout, is one line on stderr and exit 1. Success prints nothing.

It is for a container HEALTHCHECK or an exec probe in an image with no
shell or curl. --addr takes the value given to serve --addr: an
unspecified host (0.0.0.0, [::], or none, as in :8787) is probed on
loopback. It reads no token, config, or lake directory, uses no proxy,
follows no redirect, and does not retry; the orchestrator retries.

serve opens the listener only after the catalog is open and migrated,
so the probe fails while an upgrade is migrating. Give the probe a
start period long enough for that.
`

func runServeHealthcheck(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), healthcheckUsage)
		return nil
	}
	var addr string
	var timeout time.Duration
	rest, err := parseFlags(env, args, healthcheckUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&addr, "addr", "127.0.0.1:8787", "the address serve listens on")
		fs.DurationVar(&timeout, "timeout", 3*time.Second, "how long to wait for an answer")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), healthcheckUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	target, err := probeAddr(addr)
	if err != nil {
		return err
	}
	return checkHealth("http://"+target+"/healthz", timeout)
}

// probeAddr turns a listen address into one to dial. A server bound to
// every interface is reached on loopback from the same host or
// container.
func probeAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("--addr: %w", err)
	}
	if host == "" {
		host = "127.0.0.1"
	} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	}
	return net.JoinHostPort(host, port), nil
}

// checkHealth is one GET with no proxy and no redirects. The error is
// the whole report, so it names the URL.
func checkHealth(url string, timeout time.Duration) error {
	client := http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return fmt.Errorf("healthcheck: %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: %s: http %d", url, resp.StatusCode)
	}
	var got struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &got); err != nil || got.Status != "ok" {
		return fmt.Errorf("healthcheck: %s: unexpected answer %q", url, body)
	}
	return nil
}
