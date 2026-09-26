package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// runCommand runs a service manager command. Tests replace it, so no
// test reaches the real systemctl, loginctl or launchctl.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// serviceGOOS is runtime.GOOS. Tests replace it.
var serviceGOOS = runtime.GOOS

const systemdUnitName = "terva-lampi-agent.service"
const launchdLabel = "sh.terva.lampi.agent"

// installService writes the agent's user service for this binary and
// enables it: a systemd user unit on Linux, a launchd agent on macOS.
// A unit file that exists is left as it is, since it may carry local
// edits. When an agent already runs, the service is enabled but not
// started, because a second agent on the state directory would exit.
func installService(env Env, state string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	running := agentRunning(state)
	switch serviceGOOS {
	case "linux":
		return installSystemd(env, exe, running)
	case "darwin":
		return installLaunchd(env, exe, running)
	default:
		fmt.Fprintf(env.stdout(), "--install-service supports systemd and launchd; on %s, start terva-lampi agent yourself, and restart it after register\n", serviceGOOS)
		return nil
	}
}

func installSystemd(env Env, exe string, running bool) error {
	cfg := env.getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home := env.getenv("HOME")
		if home == "" {
			return errors.New("--install-service: HOME is not set")
		}
		cfg = filepath.Join(home, ".config")
	}
	path := filepath.Join(cfg, "systemd", "user", systemdUnitName)
	unit := fmt.Sprintf(`[Unit]
Description=terva-lampi capture agent
Documentation=https://github.com/terva-sh/lampi/blob/main/deploy/README.md

[Service]
Type=simple
EnvironmentFile=-%%h/.config/terva-lampi/agent.env
ExecStart=%s agent
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure

[Install]
WantedBy=default.target
`, systemdQuote(exe))
	if err := writeUnit(env, path, unit); err != nil {
		return err
	}
	if out, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %v: %s", err, bytes.TrimSpace(out))
	}
	enable := []string{"--user", "enable", systemdUnitName}
	if !running {
		enable = []string{"--user", "enable", "--now", systemdUnitName}
	}
	if out, err := runCommand("systemctl", enable...); err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(enable, " "), err, bytes.TrimSpace(out))
	}
	if running {
		fmt.Fprintf(env.stdout(), "enabled %s; an agent is already running, so the unit starts at the next login\n", systemdUnitName)
	} else {
		fmt.Fprintf(env.stdout(), "enabled and started %s\n", systemdUnitName)
	}
	user := env.getenv("USER")
	if user == "" {
		return nil
	}
	out, err := runCommand("loginctl", "show-user", user, "--property=Linger")
	if err == nil && strings.TrimSpace(string(out)) == "Linger=no" {
		fmt.Fprintf(env.stdout(), "%s has no lingering session, so the agent stops at logout; run loginctl enable-linger %s to keep it running\n", user, user)
	}
	return nil
}

func installLaunchd(env Env, exe string, running bool) error {
	home := env.getenv("HOME")
	if home == "" {
		return errors.New("--install-service: HOME is not set")
	}
	path := filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>agent</string>
	</array>
</dict>
</plist>
`, launchdLabel, xmlEscape(exe))
	if err := writeUnit(env, path, plist); err != nil {
		return err
	}
	if running {
		fmt.Fprintf(env.stdout(), "installed %s; an agent is already running, so it loads at the next login\n", path)
		return nil
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if out, err := runCommand("launchctl", "bootstrap", domain, path); err != nil {
		return fmt.Errorf("launchctl bootstrap %s %s: %v: %s", domain, path, err, bytes.TrimSpace(out))
	}
	fmt.Fprintf(env.stdout(), "loaded %s\n", launchdLabel)
	return nil
}

// writeUnit writes a unit file that does not exist yet. One that does
// is kept and named.
func writeUnit(env Env, path, body string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Fprintf(env.stdout(), "%s exists; leaving it as it is\n", path)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(env.stdout(), "wrote %s\n", path)
	return nil
}

// agentRunning reports whether an agent holds agent.pid in state.
func agentRunning(state string) bool {
	release, err := writeAgentPID(state)
	if err != nil {
		return true
	}
	release()
	return false
}

// systemdQuote quotes a path for ExecStart when it holds a space.
func systemdQuote(s string) string {
	if !strings.ContainsAny(s, " \t\"\\") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
