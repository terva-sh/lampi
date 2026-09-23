package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
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
