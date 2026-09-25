package cli

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/api"
	"terva.sh/lampi/internal/cas"
	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/protocol"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func TestAliasInstaller(t *testing.T) {
	script := filepath.Join(repoRoot(t), "deploy", "install-lampi-alias.sh")
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "terva-lampi")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho terva-lampi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	dest := filepath.Join(destDir, "lampi")

	out := runAlias(t, script, bin, dest, 0)
	if !strings.Contains(out, "primary command is still terva-lampi") {
		t.Fatalf("install output:\n%s", out)
	}
	target, err := os.Readlink(dest)
	if err != nil {
		t.Fatal(err)
	}
	if target != bin {
		t.Fatalf("symlink %s", target)
	}
	again := runAlias(t, script, bin, dest, 0)
	if !strings.Contains(again, "already points at terva-lampi") {
		t.Fatalf("second install:\n%s", again)
	}

	scriptLampi := filepath.Join(t.TempDir(), "lampi")
	body := []byte("#!/bin/sh\n# https://github.com/neurobin/lampi\n")
	if err := os.WriteFile(scriptLampi, body, 0o755); err != nil {
		t.Fatal(err)
	}
	refused := runAlias(t, script, bin, scriptLampi, 1)
	if !strings.Contains(refused, "neurobin") || !strings.Contains(refused, "left it in place") {
		t.Fatalf("neurobin warning:\n%s", refused)
	}
	got, err := os.ReadFile(scriptLampi)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatal("installer replaced the neurobin script")
	}

	other := filepath.Join(t.TempDir(), "lampi")
	if err := os.WriteFile(other, []byte("not-a-script"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := runAlias(t, script, bin, other, 1)
	if !strings.Contains(plain, "already exists") || strings.Contains(plain, "neurobin") {
		t.Fatalf("plain refusal:\n%s", plain)
	}
	got, err = os.ReadFile(other)
	if err != nil || string(got) != "not-a-script" {
		t.Fatalf("plain file changed: %q %v", got, err)
	}
}

func TestHookLeavesAMissingAgent(t *testing.T) {
	script := filepath.Join(repoRoot(t), "hooks", "terva-post-tool-enqueue.sh")
	home := t.TempDir()
	cmd := exec.Command("sh", script)
	cmd.Env = []string{"HOME=" + home, "PATH=/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(out), "not running") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestHookDoesNotSignalAForeignPID(t *testing.T) {
	script := filepath.Join(repoRoot(t), "hooks", "terva-post-tool-enqueue.sh")
	state := t.TempDir()
	dir := filepath.Join(state, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pid := filepath.Join(dir, "agent.pid")
	if err := os.WriteFile(pid, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"XDG_STATE_HOME=" + state,
		"PATH=/usr/bin:/bin",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, "not terva-lampi") {
		t.Fatalf("output:\n%s", text)
	}
	if strings.Contains(text, "asked the agent to sync") {
		t.Fatalf("hook signalled a foreign pid:\n%s", text)
	}
}

func TestHookLeavesABadPIDFile(t *testing.T) {
	state := t.TempDir()
	writeHookPID(t, state, "nope\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "not a pid") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("hook signalled a bad pid:\n%s", out)
	}
}

func TestHookLeavesADeadPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid signalling is Unix")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sleep, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	state := t.TempDir()
	writeHookPID(t, state, strconv.Itoa(pid)+"\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "not running") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("hook signalled a dead pid:\n%s", out)
	}
}

func TestHookSignalsTervaLampi(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGUSR1 hook is Unix")
	}
	cmd, _ := startCopiedSleep(t, "terva-lampi")
	exited := watchExit(t, cmd)
	state := t.TempDir()
	writeHookPID(t, state, strconv.Itoa(cmd.Process.Pid)+"\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("output:\n%s", out)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("terva-lampi stand-in still running; hook did not signal")
	}
}

func TestHookSignalsReplacedTervaLampi(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("replaced binaries keep a (deleted) /proc exe link")
	}
	cmd, bin := startCopiedSleep(t, "terva-lampi")
	exited := watchExit(t, cmd)
	if err := os.Remove(bin); err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	writeHookPID(t, state, strconv.Itoa(cmd.Process.Pid)+"\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("output:\n%s", out)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("replaced terva-lampi stand-in still running; hook did not signal")
	}
}

func TestHookSignalsLampiSymlinkOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/pid/exe distinguishes the lampi symlink")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "terva-lampi")
	copyExecutable(t, sleep, real)
	link := filepath.Join(dir, "lampi")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(link, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := watchExit(t, cmd)
	state := t.TempDir()
	writeHookPID(t, state, strconv.Itoa(cmd.Process.Pid)+"\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("output:\n%s", out)
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatal("lampi symlink stand-in still running; hook did not signal")
	}
}

func TestHookDoesNotSignalACommandThatOnlyMentionsTheName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid signalling is Unix")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(sleep, "30")
	cmd.Args = []string{"editor bin/terva-lampi", "30"}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := watchExit(t, cmd)
	state := t.TempDir()
	writeHookPID(t, state, strconv.Itoa(cmd.Process.Pid)+"\n")
	out, code := runHook(t, state)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if !strings.Contains(out, "not terva-lampi") {
		t.Fatalf("output:\n%s", out)
	}
	if strings.Contains(out, "asked the agent to sync") {
		t.Fatalf("hook signalled a foreign command:\n%s", out)
	}
	select {
	case <-exited:
		t.Fatal("hook signalled a process that only mentions terva-lampi")
	case <-time.After(300 * time.Millisecond):
	}
}

func writeHookPID(t *testing.T, state, body string) {
	t.Helper()
	dir := filepath.Join(state, "terva-lampi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.pid"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runHook(t *testing.T, state string) (string, int) {
	t.Helper()
	script := filepath.Join(repoRoot(t), "hooks", "terva-post-tool-enqueue.sh")
	cmd := exec.Command("sh", script)
	cmd.Env = []string{
		"HOME=" + t.TempDir(),
		"XDG_STATE_HOME=" + state,
		"PATH=/usr/bin:/bin",
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	return string(out), code
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func startCopiedSleep(t *testing.T, name string) (*exec.Cmd, string) {
	t.Helper()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), name)
	copyExecutable(t, sleep, bin)
	cmd := exec.Command(bin, "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd, bin
}

func watchExit(t *testing.T, cmd *exec.Cmd) <-chan struct{} {
	t.Helper()
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-exited
	})
	return exited
}

func runAlias(t *testing.T, script, bin, dest string, want int) string {
	t.Helper()
	cmd := exec.Command("sh", script, "--bin", bin, "--dest", dest)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	if code != want {
		t.Fatalf("exit %d, want %d\n%s", code, want, out)
	}
	return string(out)
}

// TestServeUnitSandbox pins the sandbox in the example serve unit.
// serve rewrites its token file beside itself, so the default data and
// token paths must sit under ReadWritePaths.
func TestServeUnitSandbox(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "systemd", "terva-lampi-serve.service"))
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]bool{}
	env := map[string]string{}
	var rw []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines[line] = true
		if v, ok := strings.CutPrefix(line, "Environment="); ok {
			k, val, _ := strings.Cut(v, "=")
			env[k] = val
		}
		if v, ok := strings.CutPrefix(line, "ReadWritePaths="); ok {
			rw = append(rw, strings.Fields(v)...)
		}
	}
	for _, want := range []string{
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"ReadWritePaths=/var/lib/terva-lampi",
		"ProtectHome=yes",
		"PrivateTmp=yes",
		"PrivateDevices=yes",
		"ProtectKernelTunables=yes",
		"ProtectKernelModules=yes",
		"ProtectKernelLogs=yes",
		"ProtectControlGroups=yes",
		"ProtectClock=yes",
		"ProtectHostname=yes",
		"RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX",
		"RestrictNamespaces=yes",
		"RestrictRealtime=yes",
		"RestrictSUIDSGID=yes",
		"LockPersonality=yes",
		"MemoryDenyWriteExecute=yes",
		"SystemCallArchitectures=native",
		"CapabilityBoundingSet=",
		"UMask=0077",
		"TimeoutStopSec=120",
	} {
		if !lines[want] {
			t.Errorf("serve unit is missing %q", want)
		}
	}
	under := func(p string) bool {
		for _, root := range rw {
			if p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/") {
				return true
			}
		}
		return false
	}
	for _, key := range []string{"LAMPI_SERVE_DATA", "LAMPI_SERVE_TOKEN_FILE"} {
		p, ok := env[key]
		if !ok {
			t.Fatalf("serve unit has no Environment=%s", key)
		}
		dir := p
		if key == "LAMPI_SERVE_TOKEN_FILE" {
			// The rewrite is a temp file in the same directory.
			dir = filepath.Dir(p)
		}
		if !under(dir) {
			t.Errorf("%s=%s: %s is not under ReadWritePaths %v", key, p, dir, rw)
		}
	}
}

// TestVPSBringupProxyAndBackup pins the nginx settings a 32 MiB blob
// needs and the order of the backup steps.
func TestVPSBringupProxyAndBackup(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "vps-bringup.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(body)
	for _, want := range []string{
		"proxy_request_buffering off;",
		"proxy_http_version 1.1;",
		"proxy_read_timeout 300s;",
		"proxy_send_timeout 300s;",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("nginx snippet is missing %q", want)
		}
	}
	_, after, ok := strings.Cut(doc, "client_max_body_size ")
	if !ok {
		t.Fatal("nginx snippet has no client_max_body_size")
	}
	size, _, _ := strings.Cut(after, "m;")
	mib, err := strconv.ParseInt(size, 10, 64)
	if err != nil {
		t.Fatalf("client_max_body_size %q: %v", size, err)
	}
	if mib<<20 <= protocol.MaxBlobBytes {
		t.Errorf("client_max_body_size %dm does not fit a %d-byte blob", mib, protocol.MaxBlobBytes)
	}

	_, backup, ok := strings.Cut(doc, "\n## Backup\n")
	if !ok {
		t.Fatal("vps-bringup.md has no Backup section")
	}
	backup, _, _ = strings.Cut(backup, "\n## ")
	last := -1
	for _, step := range []string{
		`sqlite3 /var/lib/terva-lampi/catalog.db ".backup`,
		"/var/lib/terva-lampi/cas/sha256 ",
		"/var/lib/terva-lampi/cas/logical ",
		"/var/lib/terva-lampi/tokens ",
	} {
		i := strings.Index(backup, step)
		if i < 0 {
			t.Fatalf("backup section is missing %q", step)
		}
		if i < last {
			t.Errorf("backup step %q is out of order", step)
		}
		last = i
	}
}

// TestBackupRestoresWithoutDerivedFiles follows the backup section: a
// consistent catalog copy taken while the lake is open, then the CAS,
// and no normalized/, parquet/, or cas/partial/. export on the restored
// directory rebuilds the JSONL and the parquet, and the counts match.
func TestBackupRestoresWithoutDerivedFiles(t *testing.T) {
	dir := t.TempDir()
	lake, err := api.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	lake.Allow("sekret")
	h := lake.Handler()
	good := fixturePrompt()
	sum, _, err := cas.Hash(bytes.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	putBlob(t, h, sum, good)
	ack := postManifest(t, h, manifest("sid-prompt", "sessions/x/sid-prompt.jsonl", sum, int64(len(good))))
	if err := lake.WaitNormalized(t.Context()); err != nil {
		t.Fatal(err)
	}
	want, err := lake.Catalog.Counts(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	restore := filepath.Join(t.TempDir(), "lake")
	if err := os.MkdirAll(restore, 0o700); err != nil {
		t.Fatal(err)
	}
	// VACUUM INTO is an online copy like sqlite3 .backup, without
	// needing the sqlite3 command in the test.
	db, err := sql.Open("sqlite", filepath.Join(dir, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, filepath.Join(restore, "catalog.db")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"sha256", "logical"} {
		src := filepath.Join(dir, "cas", sub)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		if err := os.CopyFS(filepath.Join(restore, "cas", sub), os.DirFS(src)); err != nil {
			t.Fatal(err)
		}
	}
	if err := lake.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "events.jsonl")
	var stderr bytes.Buffer
	if err := Run([]string{"export", "--data", restore, "--out", out}, Env{Stdout: &bytes.Buffer{}, Stderr: &stderr}); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if queryContent(t, out, "%"+proofPrompt+"%") != proofPrompt {
		t.Fatal("restored export missed the fixture prompt")
	}
	if _, err := os.Stat(filepath.Join(restore, "normalized", ack.SessionUID+".jsonl")); err != nil {
		t.Fatalf("export did not rebuild the JSONL: %v", err)
	}
	files, err := normalize.SessionParquet(filepath.Join(restore, "parquet"), ack.SessionUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("export did not rebuild the parquet")
	}

	back, err := api.Open(restore)
	if err != nil {
		t.Fatal(err)
	}
	defer back.Close()
	got, err := back.Catalog.Counts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("restored counts %+v, want %+v", got, want)
	}
}
