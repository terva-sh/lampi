package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
	"terva.sh/lampi/internal/upload"
)

const conflictsUsage = `terva-lampi conflicts — list divergent_copy artifacts

usage:
  terva-lampi conflicts [--resolved] [--data DIR]
  terva-lampi conflicts [--resolved] --server URL [--token-file PATH]
  terva-lampi conflicts [--resolved] --lake NAME [--server URL] [--token-file PATH]

Lists catalog artifacts whose relation is divergent_copy and that no
one has resolved. The head that stayed is named beside the divergent
digest. Provenance supplies the machines that posted each digest.
Nothing is merged and the head does not move. --resolved lists the
resolved ones too, each with a resolution line: kept_head, made_head,
superseded or not_a_conflict, then when and by whom.

With no --server, the command reads catalog.db in the lake directory.
That is the directory serve uses. A missing catalog file is an empty
list and is not created. --server asks that lake over GET /v1/conflicts
and sends the device token, over https, or over http only to
localhost, 127.0.0.0/8, or ::1. Pass --data or --server, not both.

The token file is --token-file, then LAMPI_TOKEN_FILE, then token_file
in config.json, then the token file in the config directory, the same
order sync and status use. The token is not an argument. The lake is
only the --server flag: LAMPI_SERVER and config.json do not turn this
command into a remote read. --lake does: it reads the lake of that name
from config.json, with its server and token file, and --server and
--token-file then override that lake's values. With more than one lake
in config.json, --server needs --lake, so the token sent is that lake's.
`

func runConflicts(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), conflictsUsage)
		return nil
	}
	var data, serverFlag, tokenFlag, lakeFlag string
	var resolved bool
	rest, err := parseFlags(env, args, conflictsUsage, func(fs *flag.FlagSet) {
		fs.BoolVar(&resolved, "resolved", false, "list resolved conflicts too")
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
		fs.StringVar(&lakeFlag, "lake", "", "lake name from config.json")
		fs.StringVar(&serverFlag, "server", "", "lake base URL")
		fs.StringVar(&tokenFlag, "token-file", "", "device token file")
	})
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		fmt.Fprint(env.stdout(), conflictsUsage)
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if data != "" && (serverFlag != "" || lakeFlag != "") {
		fmt.Fprint(env.stdout(), conflictsUsage)
		return fmt.Errorf("pass --data or a lake, not both")
	}
	// --server is resolved like --lake: with one lake it is that lake's
	// URL, as before, and with several it needs --lake, so the token
	// sent is the named lake's and not a guess.
	if lakeFlag != "" || serverFlag != "" {
		file, err := config.LoadFile(env.getenv)
		if err != nil {
			return err
		}
		lakes, err := config.ResolveLakes(file, env.getenv, config.LakeFlags{Lake: lakeFlag, Server: serverFlag, TokenFile: tokenFlag})
		if err != nil {
			return err
		}
		token, err := lakeToken(lakes[0])
		if err != nil {
			return err
		}
		body, err := fetchConflicts(lakes[0].Server.Value, token, resolved)
		if err != nil {
			return err
		}
		return writeConflicts(env.stdout(), body.Conflicts)
	}
	if data == "" {
		data, err = config.StateDir(env.getenv)
		if err != nil {
			return err
		}
	}
	return writeLocalConflicts(env.stdout(), data, resolved)
}

func writeLocalConflicts(w io.Writer, data string, resolved bool) error {
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return writeConflicts(w, nil)
		}
		return err
	}
	cat, err := catalog.OpenCurrent(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	rows, err := cat.DivergentCopies(context.Background(), resolved)
	if err != nil {
		return err
	}
	return writeConflicts(w, api.WireConflicts(rows))
}

// fetchConflicts asks for resolved conflicts only when resolved is set,
// so a lake from before resolutions, which ignores the query, answers
// the same request it always did.
func fetchConflicts(server, token string, resolved bool) (protocol.ConflictsResponse, error) {
	if err := upload.CheckToken(server, token); err != nil {
		return protocol.ConflictsResponse{}, err
	}
	client := http.Client{Timeout: 10 * time.Second}
	u := strings.TrimRight(server, "/") + "/v1/conflicts"
	if resolved {
		u += "?resolved=true"
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return protocol.ConflictsResponse{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return protocol.ConflictsResponse{}, fmt.Errorf("conflicts: unreachable (%s)", err.Error())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return protocol.ConflictsResponse{}, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return protocol.ConflictsResponse{}, fmt.Errorf("conflicts: unauthorized")
	}
	if resp.StatusCode != http.StatusOK {
		return protocol.ConflictsResponse{}, fmt.Errorf("conflicts: http %d", resp.StatusCode)
	}
	var out protocol.ConflictsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return protocol.ConflictsResponse{}, fmt.Errorf("conflicts: bad response")
	}
	if out.Conflicts == nil {
		out.Conflicts = []protocol.DivergentCopy{}
	}
	return out, nil
}

func writeConflicts(w io.Writer, rows []protocol.DivergentCopy) error {
	if rows == nil {
		rows = []protocol.DivergentCopy{}
	}
	fmt.Fprintf(w, "divergent_copy: %d\n", len(rows))
	for _, row := range rows {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "session_uid: %s\n", row.SessionUID)
		fmt.Fprintf(w, "artifact_id: %s\n", row.ArtifactID)
		fmt.Fprintf(w, "harness: %s\n", row.Harness)
		fmt.Fprintf(w, "native_session_id: %s\n", row.NativeSessionID)
		fmt.Fprintf(w, "kind: %s\n", row.Kind)
		fmt.Fprintf(w, "relpath: %s\n", row.RelPath)
		fmt.Fprintf(w, "sha256: %s\n", row.SHA256)
		fmt.Fprintf(w, "size: %d\n", row.Size)
		fmt.Fprintf(w, "head_sha256: %s\n", row.HeadSHA256)
		fmt.Fprintf(w, "head_size: %d\n", row.HeadSize)
		fmt.Fprintf(w, "machines: %s\n", strings.Join(row.Machines, ", "))
		fmt.Fprintf(w, "head_machines: %s\n", strings.Join(row.HeadMachines, ", "))
		if res := row.Resolution; res != nil {
			fmt.Fprintf(w, "resolution: %s at %s by %s\n", res.Resolution, res.ResolvedAt, res.ResolvedBy)
		}
	}
	return nil
}
