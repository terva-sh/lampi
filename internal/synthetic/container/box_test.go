//go:build synthetic_container

// Package container is the Layer B smoke driver for image
// terva-lampi:synthetic. make synthetic-container builds that image
// from e2e/Dockerfile and does not run these tests.
package container

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"terva.sh/lampi/internal/config"
	"terva.sh/lampi/internal/protocol"
)

const (
	imageRef    = "terva-lampi:synthetic"
	binPath     = "/usr/local/bin/terva-lampi"
	serverURL   = "http://127.0.0.1:8787"
	lakeMount   = "/lake"
	stateMount  = "/state"
	configMount = "/config"
	listenLine  = "terva-lampi serve: listening on 127.0.0.1:8787"
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

// box is one container and the host directories mounted into it.
// roots are host paths. status rows use the container paths.
type box struct {
	name  string
	data  string
	state string
	roots map[string]string
	rows  []statusRow
}

// requireRuntime skips when docker or the synthetic image is missing.
// A missing image is not a failure: make synthetic-container tags it,
// and these tests do not build it.
func requireRuntime(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	if out, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Skipf("docker daemon unavailable: %s", firstLine(out, err))
	}
	out, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", imageRef).CombinedOutput()
	if err != nil {
		t.Skipf("image %s not present: %s", imageRef, firstLine(out, err))
	}
}

func firstLine(out []byte, err error) string {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return err.Error()
	}
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// openBox mounts a planted world and starts one container. Nothing is
// published: clients use 127.0.0.1 inside the container via docker exec.
// The image has no shell, so a static keeper is pid 1 and starts serve.
func openBox(t *testing.T, allowPrefix string, disabled ...string) *box {
	t.Helper()
	requireRuntime(t)
	keeper, err := buildKeeper()
	if err != nil {
		t.Fatal(err)
	}

	off := map[string]bool{
		protocol.HarnessCursor:    true,
		protocol.HarnessCursorCLI: true,
	}
	for _, id := range disabled {
		off[id] = true
	}

	b := &box{
		name:  containerName(),
		data:  t.TempDir(),
		state: t.TempDir(),
		roots: map[string]string{},
		rows:  make([]statusRow, 0, len(statusOrder)),
	}
	cfg := t.TempDir()
	homes := t.TempDir()
	for _, id := range planted {
		dir := filepath.Join(homes, id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		b.roots[id] = dir
	}
	for _, id := range statusOrder {
		row := statusRow{id: id, enabled: !off[id]}
		if _, ok := b.roots[id]; ok {
			row.root = homeMount(id)
			row.source = "config"
		} else {
			root, ok := linuxDefaultRoot(id)
			if !ok {
				t.Fatalf("no container root for %s", id)
			}
			row.root = root
			row.source = "default"
		}
		b.rows = append(b.rows, row)
	}
	if err := writeConfig(cfg, allowPrefix, b.rows); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if t.Failed() {
			t.Log(b.debug())
		}
		out, err := exec.Command("docker", "rm", "-f", b.name).CombinedOutput()
		if err != nil && !strings.Contains(string(out), "No such container") {
			t.Logf("docker rm: %v\n%s", err, out)
		}
	})

	args := []string{"run", "-d", "--name", b.name, "--rm", "--entrypoint", "/keeper"}
	if uid := os.Getuid(); uid >= 0 {
		args = append(args, "--user", fmt.Sprintf("%d:%d", uid, os.Getgid()))
	}
	args = append(args,
		"-e", "XDG_STATE_HOME="+stateMount,
		"-e", "XDG_CONFIG_HOME="+configMount,
		"-v", b.data+":"+lakeMount,
		"-v", b.state+":"+stateMount,
		"-v", cfg+":"+configMount,
		"-v", keeper+":/keeper:ro",
	)
	for _, id := range planted {
		args = append(args, "-v", b.roots[id]+":"+homeMount(id))
	}
	// "hold" replaces the image CMD. The keeper starts serve itself.
	args = append(args, imageRef, "hold")

	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	b.assertPrivate(t)
	b.waitReady(t)
	return b
}

// linuxDefaultRoot is the adapter default inside the Linux image when
// XDG_CONFIG_HOME=/config. Home() follows the test process GOOS, which
// is not the container.
func linuxDefaultRoot(id string) (string, bool) {
	switch id {
	case protocol.HarnessCursor:
		return path.Join(configMount, "Cursor"), true
	case protocol.HarnessCursorCLI:
		return path.Join(configMount, "cursor"), true
	default:
		return "", false
	}
}

func homeMount(id string) string {
	return path.Join("/homes", id)
}

func writeConfig(dir, allowPrefix string, rows []statusRow) error {
	f := config.File{
		Server:    serverURL,
		Harnesses: config.Harnesses{},
	}
	if allowPrefix != "" {
		f.Projects.Allow = []config.ProjectMatch{{CWDPrefix: allowPrefix}}
	}
	for _, row := range rows {
		e := config.HarnessConfig{Enabled: row.enabled}
		if row.source == "config" {
			e.Root = row.root
		}
		f.Harnesses[row.id] = e
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	cfgDir := filepath.Join(dir, "terva-lampi")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cfgDir, "config.json"), raw, 0o600)
}

func (b *box) assertPrivate(t *testing.T) {
	t.Helper()
	ports := strings.TrimSpace(b.inspect(t, "{{json .HostConfig.PortBindings}}"))
	if ports != "{}" && ports != "null" {
		t.Fatalf("container published ports: %s", ports)
	}
	if strings.TrimSpace(b.inspect(t, "{{.HostConfig.PublishAllPorts}}")) == "true" {
		t.Fatal("container publishes all ports")
	}
	if strings.TrimSpace(b.inspect(t, "{{.HostConfig.NetworkMode}}")) == "host" {
		t.Fatal("container uses host network")
	}
}

func (b *box) inspect(t *testing.T, format string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f", format, b.name).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect: %v\n%s", err, out)
	}
	return string(out)
}

func (b *box) waitReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		if !b.running() {
			t.Fatalf("container exited\n%s", b.debug())
		}
		if strings.Contains(b.serveLog(), listenLine) {
			stdout, stderr, err := b.lampi("status")
			last = stdout + stderr
			if err == nil && strings.Contains(stdout, "health: ok") {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("serve not healthy\nstatus:\n%s\n%s", last, b.debug())
}

func (b *box) running() bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", b.name).Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// stopServe asks the keeper to SIGTERM serve and waits until the
// catalog is closed. Export is the second writer.
func (b *box) stopServe(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(b.state, "stop-serve"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(filepath.Join(b.state, "serve.stopped"))
		if err == nil && strings.TrimSpace(string(body)) == "ok" {
			return
		}
		if !b.running() {
			t.Fatalf("container exited while stopping serve\n%s", b.debug())
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("serve did not exit\n%s", b.debug())
}

func (b *box) exportEvents(t *testing.T) string {
	t.Helper()
	b.stopServe(t)
	stdout, stderr, err := b.lampi("export", "--data", lakeMount, "--out", lakeMount+"/events.jsonl", "--format", "events")
	if err != nil {
		t.Fatalf("export: %v\nstdout:\n%s\nstderr:\n%s\n%s", err, stdout, stderr, b.debug())
	}
	if stderr != "" {
		t.Fatalf("export stderr: %s", stderr)
	}
	body, err := os.ReadFile(filepath.Join(b.data, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func (b *box) lampi(args ...string) (string, string, error) {
	cmd := exec.Command("docker", append([]string{"exec", b.name, binPath}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func (b *box) serveLog() string {
	body, err := os.ReadFile(filepath.Join(b.state, "serve.log"))
	if err != nil {
		return ""
	}
	return string(body)
}

func (b *box) debug() string {
	logs, _ := exec.Command("docker", "logs", b.name).CombinedOutput()
	state, _ := exec.Command("docker", "inspect", "-f", "running={{.State.Running}} exit={{.State.ExitCode}} error={{.State.Error}}", b.name).CombinedOutput()
	return fmt.Sprintf("state: %s\nkeeper logs:\n%s\nserve.log:\n%s", bytes.TrimSpace(state), logs, b.serveLog())
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

func containerName() string {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("lampi-syn-%d", time.Now().UnixNano())
	}
	return "lampi-syn-" + hex.EncodeToString(buf[:])
}

var (
	keeperOnce sync.Once
	keeperBin  string
	keeperErr  error
)

// TestMain removes the Once cache directory after the suite. The binary
// stays for every test that calls buildKeeper. Clearing it from the
// first test would delete the cache while later tests still need it.
func TestMain(m *testing.M) {
	code := m.Run()
	if keeperBin != "" {
		_ = os.RemoveAll(filepath.Dir(keeperBin))
	}
	os.Exit(code)
}

func buildKeeper() (string, error) {
	keeperOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			keeperErr = err
			return
		}
		dir, err := os.MkdirTemp("", "lampi-keeper-")
		if err != nil {
			keeperErr = err
			return
		}
		bin := filepath.Join(dir, "keeper")
		// Set before the build so TestMain can remove the temp dir when
		// the compile fails and never reaches the success assignment.
		keeperBin = bin
		cmd := exec.Command("go", "build", "-buildvcs=false", "-tags", "synthetic_container", "-trimpath", "-o", bin, "./internal/synthetic/container/keeper")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH="+runtime.GOARCH)
		out, err := cmd.CombinedOutput()
		if err != nil {
			keeperErr = fmt.Errorf("build keeper: %w\n%s", err, out)
			return
		}
		if err := os.Chmod(bin, 0o755); err != nil {
			keeperErr = err
			return
		}
	})
	return keeperBin, keeperErr
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
