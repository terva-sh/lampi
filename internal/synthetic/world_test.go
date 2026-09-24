package synthetic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"terva.sh/lampi/internal/adapter/cursor"
	"terva.sh/lampi/internal/adapter/cursorcli"
	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cli"
	"terva.sh/lampi/internal/protocol"
)

// statusOrder is the order terva-lampi status prints harness lines.
var statusOrder = []string{
	protocol.HarnessTerva,
	protocol.HarnessClaude,
	protocol.HarnessCodex,
	protocol.HarnessOpenCode,
	protocol.HarnessCursor,
	protocol.HarnessCursorCLI,
}

// planted are the four harnesses this suite configures with a root.
// cursor and cursor-cli stay disabled and keep the adapter default.
var planted = []string{
	protocol.HarnessTerva,
	protocol.HarnessClaude,
	protocol.HarnessCodex,
	protocol.HarnessOpenCode,
}

// world is one synthetic machine. data is the lake directory export
// reads with --data. state is XDG_STATE_HOME, where sync keeps the
// outbox. Those directories are not the same.
type world struct {
	data   string
	state  string
	config string
	home   string
	server string
	roots  map[string]string
	rows   []statusRow
	lake   *api.Server
	srv    *httptest.Server
}

type statusRow struct {
	id      string
	enabled bool
	root    string
	source  string
}

func (r statusRow) line() string {
	return fmt.Sprintf("harness %s enabled=%t root=%s source=%s", r.id, r.enabled, r.root, r.source)
}

type syncCounts struct {
	checked     int
	missing     int
	uploaded    int
	manifests   int
	refused     int
	quarantined int
}

// openWorld writes config.json and starts an unauthenticated httptest
// lake. allowPrefix empty is an empty allow list. disabled harnesses
// are off in addition to cursor and cursor-cli. No token file is written.
func openWorld(t *testing.T, allowPrefix string, disabled ...string) *world {
	t.Helper()
	off := map[string]bool{
		protocol.HarnessCursor:    true,
		protocol.HarnessCursorCLI: true,
	}
	for _, id := range disabled {
		off[id] = true
	}

	w := &world{
		data:   t.TempDir(),
		state:  t.TempDir(),
		config: t.TempDir(),
		home:   t.TempDir(),
		roots:  map[string]string{},
	}
	w.rows = make([]statusRow, 0, len(statusOrder))
	for _, id := range planted {
		w.roots[id] = t.TempDir()
	}

	lake, err := api.Open(w.data)
	if err != nil {
		t.Fatal(err)
	}
	w.lake = lake
	w.srv = httptest.NewServer(lake.Handler())
	w.server = w.srv.URL
	t.Cleanup(func() {
		if w.srv != nil {
			w.srv.Close()
			w.srv = nil
		}
		if w.lake != nil {
			if err := w.lake.Close(); err != nil {
				t.Errorf("lake close: %v", err)
			}
			w.lake = nil
		}
	})

	getenv := w.getenv()
	for _, id := range statusOrder {
		row := statusRow{id: id, enabled: !off[id]}
		if root, ok := w.roots[id]; ok {
			row.root = root
			row.source = "config"
		} else {
			row.root = defaultRoot(t, id, getenv)
			row.source = "default"
		}
		w.rows = append(w.rows, row)
	}
	if err := w.writeConfig(allowPrefix); err != nil {
		t.Fatal(err)
	}
	return w
}

func defaultRoot(t *testing.T, id string, getenv func(string) string) string {
	t.Helper()
	var (
		root string
		err  error
	)
	switch id {
	case protocol.HarnessCursor:
		root, err = cursor.Home(getenv)
	case protocol.HarnessCursorCLI:
		root, err = cursorcli.Home(getenv)
	default:
		t.Fatalf("no default root for %s", id)
	}
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func (w *world) getenv() func(string) string {
	return func(k string) string {
		switch k {
		case "HOME":
			return w.home
		case "XDG_CONFIG_HOME":
			return w.config
		case "XDG_STATE_HOME":
			return w.state
		case "LAMPI_SERVER":
			return w.server
		default:
			return ""
		}
	}
}

func (w *world) writeConfig(allowPrefix string) error {
	type match struct {
		Prefix string `json:"cwd_prefix"`
	}
	type projects struct {
		Allow []match `json:"allow"`
	}
	type entry struct {
		Enabled *bool  `json:"enabled,omitempty"`
		Root    string `json:"root,omitempty"`
	}
	type file struct {
		Server    string           `json:"server"`
		Projects  *projects        `json:"projects,omitempty"`
		Harnesses map[string]entry `json:"harnesses"`
	}
	f := file{
		Server:    w.server,
		Harnesses: map[string]entry{},
	}
	if allowPrefix != "" {
		f.Projects = &projects{Allow: []match{{Prefix: allowPrefix}}}
	}
	for _, row := range w.rows {
		e := entry{}
		if row.source == "config" {
			e.Root = row.root
		}
		if !row.enabled {
			e.Enabled = new(false)
		}
		f.Harnesses[row.id] = e
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Join(w.config, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600)
}

func (w *world) run(args ...string) (stdout, stderr string, err error) {
	var out, errb bytes.Buffer
	err = cli.Run(args, cli.Env{
		Stdout: &out,
		Stderr: &errb,
		Getenv: w.getenv(),
	})
	return out.String(), errb.String(), err
}

// closeLake releases the catalog so export can open the same data
// directory. sqlite allows one writer.
func (w *world) closeLake(t *testing.T) {
	t.Helper()
	if w.srv != nil {
		w.srv.Close()
		w.srv = nil
	}
	if w.lake != nil {
		if err := w.lake.Close(); err != nil {
			t.Fatal(err)
		}
		w.lake = nil
	}
}

func (w *world) exportEvents(t *testing.T) string {
	t.Helper()
	w.closeLake(t)
	out := filepath.Join(t.TempDir(), "events.jsonl")
	stdout, stderr, err := w.run("export", "--data", w.data, "--out", out, "--format", "events")
	if err != nil {
		t.Fatalf("export: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("export stderr: %s", stderr)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func harnessLines(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "harness ") {
			out = append(out, line)
		}
	}
	return out
}

func parseSync(t *testing.T, stdout string) syncCounts {
	t.Helper()
	var line string
	for _, l := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(l, "checked ") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("sync summary missing:\n%s", stdout)
	}
	var c syncCounts
	_, err := fmt.Sscanf(line, "checked %d, missing %d, uploaded %d, manifests %d, refused %d, quarantined %d",
		&c.checked, &c.missing, &c.uploaded, &c.manifests, &c.refused, &c.quarantined)
	if err != nil {
		t.Fatalf("sync summary %q: %v", line, err)
	}
	return c
}

func casFiles(t *testing.T, data string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(data, "cas"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMultiHarnessStatus(t *testing.T) {
	w := openWorld(t, "/work")
	stdout, stderr, err := w.run("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("status stderr: %s", stderr)
	}
	var want []string
	for _, row := range w.rows {
		want = append(want, row.line())
	}
	got := harnessLines(stdout)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("harness lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(stdout, "health: ok") || !strings.Contains(stdout, "catalog_sessions: 0") {
		t.Fatalf("status:\n%s", stdout)
	}
}
