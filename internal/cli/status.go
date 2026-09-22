package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/discover"
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
	"terva.sh/lampi/internal/watermark"
)

const statusUsage = `terva-lampi status — agent state and lake health

usage:
  terva-lampi status [--server URL] [--token-file PATH]

Prints the local machine id (creating it if needed), outbox depth, a
watermark summary, and the last finished sync. Those live under the
state directory, next to the files the agent and sync already use.

GET /healthz reports whether the lake process is up. It carries no
catalog data and does not need the token. GET /v1/stats reports how
many sessions, artifacts, and machines the catalog holds, and uses the
device token when one is configured. A lake that does not answer is
reported; the local lines are still printed.
`

func runStatus(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), statusUsage)
		return nil
	}
	var serverFlag, tokenFlag string
	rest, err := parseFlags(env, args, statusUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&serverFlag, "server", "", "lake base URL")
		fs.StringVar(&tokenFlag, "token-file", "", "device token file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), statusUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	m, err := config.EnsureMachine(env.getenv)
	if err != nil {
		return err
	}
	home, err := discover.TervaHome(env.getenv)
	if err != nil {
		return err
	}
	files, err := discover.Sessions(home)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	server := config.ServerURL(file, serverFlag)
	tokenPath, err := tokenPathFor(env, tokenFlag, file)
	if err != nil {
		return err
	}
	token, err := resolveToken(env, tokenFlag, file)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", m.MachineID)
	if m.Hostname != "" {
		fmt.Fprintf(env.stdout(), "hostname: %s\n", m.Hostname)
	}
	fmt.Fprintf(env.stdout(), "terva_home: %s\n", home)
	fmt.Fprintf(env.stdout(), "sessions: %d\n", len(files))
	if err := writeCaptureState(env.stdout(), state); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "server: %s\n", server)
	fmt.Fprintf(env.stdout(), "token_file: %s\n", tokenPath)
	fmt.Fprintf(env.stdout(), "health: %s\n", probeHealth(server))
	fmt.Fprint(env.stdout(), probeCatalog(server, token))
	return nil
}

// writeCaptureState prints outbox depth, the watermark summary, and the
// last finished sync. A database that has not been created yet is zero.
// Opening one would create it, and status is a read.
func writeCaptureState(w io.Writer, stateDir string) error {
	depth, err := outboxDepth(stateDir)
	if err != nil {
		return err
	}
	sum, err := watermarkSummary(stateDir)
	if err != nil {
		return err
	}
	last, err := lastSyncLine(stateDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "outbox: %d\n", depth)
	fmt.Fprintln(w, formatWatermarks(sum))
	fmt.Fprintln(w, last)
	return nil
}

func outboxDepth(stateDir string) (int, error) {
	path := outbox.File(stateDir)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	q, err := outbox.Open(path)
	if err != nil {
		return 0, err
	}
	defer q.Close()
	return q.Depth(context.Background())
}

func watermarkSummary(stateDir string) (watermark.Summary, error) {
	path := watermark.File(stateDir)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return watermark.Summary{}, nil
		}
		return watermark.Summary{}, err
	}
	db, err := watermark.Open(path)
	if err != nil {
		return watermark.Summary{}, err
	}
	defer db.Close()
	return db.Summary(context.Background())
}

func formatWatermarks(s watermark.Summary) string {
	if s.Paths == 0 {
		return "watermarks: 0"
	}
	line := fmt.Sprintf("watermarks: %d paths, %d bytes", s.Paths, s.Bytes)
	if !s.Newest.IsZero() {
		line += ", newest " + s.Newest.UTC().Format(time.RFC3339)
	}
	return line
}

func lastSyncLine(stateDir string) (string, error) {
	st, ok, err := upload.ReadLastSync(stateDir)
	if err != nil {
		return "", err
	}
	if !ok {
		return "last_sync: never", nil
	}
	return fmt.Sprintf("last_sync: %s uploaded=%d manifests=%d",
		st.At.UTC().Format(time.RFC3339), st.Uploaded, st.Manifests), nil
}

func probeHealth(server string) string {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(server, "/") + "/healthz")
	if err != nil {
		return "unreachable (" + err.Error() + ")"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("http %d", resp.StatusCode)
	}
	if strings.Contains(string(body), `"ok"`) {
		return "ok"
	}
	return "http 200"
}

func probeCatalog(server, token string) string {
	client := http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+"/v1/stats", nil)
	if err != nil {
		return "catalog: " + err.Error() + "\n"
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "catalog: unreachable (" + err.Error() + ")\n"
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized {
		return "catalog: unauthorized\n"
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("catalog: http %d\n", resp.StatusCode)
	}
	var counts protocol.StatsResponse
	if err := json.Unmarshal(body, &counts); err != nil {
		return "catalog: bad response\n"
	}
	return fmt.Sprintf("catalog_sessions: %d\ncatalog_artifacts: %d\ncatalog_machines: %d\n",
		counts.Sessions, counts.Artifacts, counts.Machines)
}
