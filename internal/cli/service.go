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

// systemdUserDir is the directory the running user manager searches for
// a user's own units: its $XDG_CONFIG_HOME/systemd/user as it was when
// it built its UnitPath. Neither this process's XDG_CONFIG_HOME
// (register may run with it set elsewhere, TKT-01M3GAHSQ) nor
// show-environment (set-environment changes it later) reliably says
// where that is. systemd lists the persistent control directory,
// <that directory>.control, first in UnitPath, so the directory is the
// output up to the first "/systemd/user.control", without ".control".
// Reading from the start keeps a path with a space whole, since systemd
// separates entries with spaces and does not quote them; an entry that
// is not a single absolute path is refused. When UnitPath cannot be
// read or does not start that way, ~/.config/systemd/user is used and
// enable reports any problem. The unit's Environment= lines carry this
// process's directories to the agent either way.
func systemdUserDir(env Env) (string, error) {
	const control = "/systemd/user.control"
	if out, err := runCommand("systemctl", "--user", "show", "--property=UnitPath", "--value"); err == nil {
		list := strings.TrimRight(string(out), "\r\n")
		if i := strings.Index(list, control); i > 0 {
			first := list[:i+len(control)]
			rest := list[i+len(control):]
			if filepath.IsAbs(first) && !strings.Contains(first, " /") && (rest == "" || rest[0] == ' ') {
				return strings.TrimSuffix(first, ".control"), nil
			}
		}
	}
	home := env.getenv("HOME")
	if home == "" {
		return "", errors.New("--install-service: HOME is not set")
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func installSystemd(env Env, exe string, running bool) error {
	dir, err := systemdUserDir(env)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, systemdUnitName)
	unit := fmt.Sprintf(`[Unit]
Description=terva-lampi capture agent
Documentation=https://github.com/terva-sh/lampi/blob/main/deploy/README.md

[Service]
Type=simple
%sEnvironmentFile=-%%h/.config/terva-lampi/agent.env
ExecStart=%s agent
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure

[Install]
WantedBy=default.target
`, systemdEnv(serviceEnv(env)), systemdQuote(exe))
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
%s</dict>
</plist>
`, launchdLabel, xmlEscape(exe), launchdEnv(serviceEnv(env)))
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

// serviceEnv is each XDG directory this process has set away from its
// default, made absolute. register wrote the config, token and state
// there, and the agent reads XDG_DATA_HOME for OpenCode's sessions, so
// the service gets the same values. One unset or at its default is
// left out, and the unit stays as it was.
func serviceEnv(env Env) [][2]string {
	home := env.getenv("HOME")
	var vars [][2]string
	for _, v := range []struct{ key, def string }{
		{"XDG_CONFIG_HOME", ".config"},
		{"XDG_STATE_HOME", filepath.Join(".local", "state")},
		{"XDG_DATA_HOME", filepath.Join(".local", "share")},
	} {
		val := env.getenv(v.key)
		if val == "" {
			continue
		}
		if abs, err := filepath.Abs(val); err == nil {
			val = abs
		}
		if home != "" && val == filepath.Join(home, v.def) {
			continue
		}
		vars = append(vars, [2]string{v.key, val})
	}
	return vars
}

// systemdEnv is one quoted Environment= line per variable. systemd
// expands specifiers and C escapes there, so % is doubled and \, " and
// newlines are escaped.
func systemdEnv(vars [][2]string) string {
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%", "\n", `\n`)
	var b strings.Builder
	for _, v := range vars {
		fmt.Fprintf(&b, "Environment=\"%s\"\n", esc.Replace(v[0]+"="+v[1]))
	}
	return b.String()
}

// launchdEnv is the plist's EnvironmentVariables dict, or nothing.
func launchdEnv(vars [][2]string) string {
	if len(vars) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	for _, v := range vars {
		fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", v[0], xmlEscape(v[1]))
	}
	b.WriteString("\t</dict>\n")
	return b.String()
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
