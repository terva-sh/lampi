package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
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
