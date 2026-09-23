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

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

const conflictsUsage = `terva-lampi conflicts — list divergent_copy artifacts

usage:
  terva-lampi conflicts [--data DIR]
  terva-lampi conflicts --server URL [--token-file PATH]

Lists catalog artifacts whose relation is divergent_copy. The head
that stayed is named beside the divergent digest. Provenance supplies
the machines that posted each digest. Nothing is merged and the head
does not move.

With no --server, the command reads catalog.db in the lake directory.
That is the directory serve uses. A missing catalog file is an empty
list and is not created. --server asks that lake over GET /v1/conflicts
and sends the device token. Pass --data or --server, not both.

The token is read from --token-file. It is not an argument.
`

func runConflicts(env Env, args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(env.stdout(), conflictsUsage)
		return nil
	}
	var data, serverFlag, tokenFlag string
	rest, err := parseFlags(env, args, conflictsUsage, func(fs *flag.FlagSet) {
		fs.StringVar(&data, "data", "", "lake directory (default: state dir)")
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
	if data != "" && serverFlag != "" {
		fmt.Fprint(env.stdout(), conflictsUsage)
		return fmt.Errorf("pass --data or --server, not both")
	}
	if serverFlag != "" {
		return writeRemoteConflicts(env, serverFlag, tokenFlag)
	}
	if data == "" {
		data, err = config.StateDir(env.getenv)
		if err != nil {
			return err
		}
	}
	return writeLocalConflicts(env.stdout(), data)
}

func writeLocalConflicts(w io.Writer, data string) error {
	path := filepath.Join(data, "catalog.db")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return writeConflicts(w, nil)
		}
		return err
	}
	cat, err := catalog.Open(path)
	if err != nil {
		return err
	}
	defer cat.Close()
	rows, err := cat.DivergentCopies(context.Background())
	if err != nil {
		return err
	}
	return writeConflicts(w, asProtocolConflicts(rows))
}

func writeRemoteConflicts(env Env, server, tokenFlag string) error {
	file, err := config.LoadFile(env.getenv)
	if err != nil {
		return err
	}
	token, err := resolveToken(env, tokenFlag, file)
	if err != nil {
		return err
	}
	body, err := fetchConflicts(server, token)
	if err != nil {
		return err
	}
	return writeConflicts(env.stdout(), body.Conflicts)
}

func fetchConflicts(server, token string) (protocol.ConflictsResponse, error) {
	client := http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(server, "/")+"/v1/conflicts", nil)
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

func asProtocolConflicts(rows []catalog.DivergentCopy) []protocol.DivergentCopy {
	out := make([]protocol.DivergentCopy, 0, len(rows))
	for _, row := range rows {
		out = append(out, protocol.DivergentCopy{
			SessionUID:      row.SessionUID,
			ArtifactID:      row.ArtifactID,
			Harness:         row.Harness,
			NativeSessionID: row.NativeID,
			Kind:            row.Kind,
			RelPath:         row.RelPath,
			SHA256:          row.SHA256,
			Size:            row.Size,
			HeadSHA256:      row.HeadSHA256,
			HeadSize:        row.HeadSize,
			Machines:        row.Machines,
			HeadMachines:    row.HeadMachines,
		})
	}
	return out
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
	}
	return nil
}
