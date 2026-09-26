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
	"terva.sh/lampi/internal/outbox"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
	"terva.sh/lampi/internal/watermark"
)

const statusUsage = `terva-lampi status — agent state and lake health

usage:
  terva-lampi status [--lake NAME] [--server URL] [--token-file PATH]

--server is the flag, then LAMPI_SERVER, then server in config.json,
then http://127.0.0.1:8787. --token-file is the flag, then
LAMPI_TOKEN_FILE, then token_file in config.json, then the token file
in the config directory. The agent and sync resolve both the same
way. The server and token_file lines end in source=flag, env, config,
or default, naming the layer that won.

Prints the local machine id (creating it if needed), one line per
known harness, outbox depth, a watermark summary, the last finished
sync, and the last attempt with the most recent error. last_attempt
is the time of the last run and ok or failed. last_error is the time
and text of the most recent failure, or none. last_skipped is how
many files or harnesses that run left out because they could not be
read, followed by up to five of them, one per indented line. Those live under the state directory, next to the
files the agent and sync already use.

Each harness line has this spelling, in order terva, claude, codex,
opencode, cursor, cursor-cli:

  harness <id> enabled=<true|false> root=<absolute path or empty> source=<config|env|default>

root is empty when the directory cannot be named. source is config
when the config.json root won, env when that harness's environment
variable won, and default otherwise. enabled false still prints the
root and source. An omitted harnesses map, or an omitted id, is
enabled true. sessions counts files from harnesses that are on.

The Cursor IDE global database has an empty cwd and sync refuses it
by design. A workspace export copies that global database read-only
and merges cursorDiskKV rows for composers named by that workspace's
composer.composerHeaders. The session id stays workspace/<id>. A
workspace database takes its cwd from workspace.json. A
Cursor CLI chat needs an absolute cwd in the sibling meta.json, or
sync refuses that export. Those refusals are named on sync stderr.
The projects allow and deny rules are unchanged.

status shows one lake: the one --lake names, or the default lake, or
the first lake by name when there is no default. The lake line names
it, and other_lakes counts the rest. The capture state lines (outbox,
watermarks, last sync) are this machine's, shared by every lake until
each lake has its own state.

GET /healthz reports whether the lake process is up. It carries no
catalog data and does not need the token. GET /v1/stats reports how
many sessions, artifacts, and machines the catalog holds, and uses the
device token when one is configured. The token is not sent to an
http:// URL whose host is not localhost, 127.0.0.0/8, or ::1; the
catalog line says so instead. A lake that does not answer is
reported, and a last-sync stamp that cannot be read is reported as
unreadable. The other lines are still printed.
`

func runStatus(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), statusUsage)
		return nil
	}
	var serverFlag, tokenFlag, lakeFlag string
	rest, err := parseFlags(env, args, statusUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&lakeFlag, "lake", "", "lake name from config.json")
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
	_, n, err := countSources(env.getenv, file.Harnesses)
	if err != nil {
		return err
	}
	state, err := config.StateDir(env.getenv)
	if err != nil {
		return err
	}
	lakes, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{Lake: lakeFlag, Server: serverFlag, TokenFile: tokenFlag})
	if err != nil {
		return err
	}
	if len(lakes) == 0 {
		return fmt.Errorf("no lake is configured")
	}
	lake := lakes[0]
	server, tokenFile := lake.Server, lake.TokenFile
	token, err := lakeToken(lake)
	if err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "machine_id: %s\n", m.MachineID)
	if m.Hostname != "" {
		fmt.Fprintf(env.stdout(), "hostname: %s\n", m.Hostname)
	}
	for _, h := range harnessStatuses(env.getenv, file.Harnesses) {
		fmt.Fprintln(env.stdout(), h.line())
	}
	fmt.Fprintf(env.stdout(), "sessions: %d\n", n)
	if err := writeCaptureState(env.stdout(), state); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "lake: %s\n", lake.Name)
	if len(lakes) > 1 {
		fmt.Fprintf(env.stdout(), "other_lakes: %d (pass --lake to show one)\n", len(lakes)-1)
	}
	writeEndpoint(env.stdout(), server, tokenFile)
	fmt.Fprintf(env.stdout(), "health: %s\n", probeHealth(server.Value))
	fmt.Fprint(env.stdout(), probeCatalog(server.Value, token))
	return nil
}

// writeEndpoint prints the server and token file with the layer that
// won each, in the spelling the harness lines use.
func writeEndpoint(w io.Writer, server, tokenFile config.Setting) {
	fmt.Fprintf(w, "server: %s source=%s\n", server.Value, server.Source)
	fmt.Fprintf(w, "token_file: %s source=%s\n", tokenFile.Value, tokenFile.Source)
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
	fmt.Fprintf(w, "outbox: %d\n", depth)
	fmt.Fprintln(w, formatWatermarks(sum))
	fmt.Fprintln(w, lastSyncLine(stateDir))
	fmt.Fprint(w, lastAttemptLines(stateDir))
	return nil
}

// lastAttemptLines is the last run, finished or not, and the most
// recent error. A finished run after a failure still shows that error
// with its time.
func lastAttemptLines(stateDir string) string {
	a, ok, err := upload.ReadAttempt(stateDir)
	if err != nil {
		return "last_attempt: unreadable\n"
	}
	if !ok {
		return "last_attempt: never\nlast_error: none\n"
	}
	result := "ok"
	if a.Error != "" {
		result = "failed"
	}
	out := fmt.Sprintf("last_attempt: %s %s\n", a.At.UTC().Format(time.RFC3339), result)
	if a.LastError == "" {
		out += "last_error: none\n"
	} else {
		// An error can span lines, such as a refusal list. status keeps
		// one line per field.
		msg := strings.Join(strings.Fields(a.LastError), " ")
		out += fmt.Sprintf("last_error: %s %s\n", a.LastErrorAt.UTC().Format(time.RFC3339), msg)
	}
	out += fmt.Sprintf("last_skipped: %d\n", a.Skipped)
	for _, line := range a.SkippedLines {
		out += "  " + line + "\n"
	}
	return out
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

func lastSyncLine(stateDir string) string {
	st, ok, err := upload.ReadLastSync(stateDir)
	if err != nil {
		// A torn or corrupt stamp must not hide the rest of status.
		// The lake probe works the same way: report it, keep going.
		return "last_sync: unreadable"
	}
	if !ok {
		return "last_sync: never"
	}
	return fmt.Sprintf(
		"last_sync: %s uploaded=%d manifests=%d refused=%d quarantined=%d",
		st.At.UTC().Format(time.RFC3339),
		st.Uploaded, st.Manifests, st.Refused, st.Quarantined)
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
	if err := upload.CheckToken(server, token); err != nil {
		return "catalog: " + err.Error() + "\n"
	}
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
