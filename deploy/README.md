# Packaging examples

These files are examples. `make build` does not install them. Copy the
ones you want, and point them at the `terva-lampi` binary you built.
The hook at the bottom is supported and optional. Nothing installs it
either. The [Hook](#hook) section is how to wire it.

Phase 0 places the lake on a small VPS.
[docs/policy.md](../docs/policy.md) is that decision: TLS in front of
`serve`, device tokens, no TTL, and volume encryption or LUKS. The
operator checklist is [docs/vps-bringup.md](../docs/vps-bringup.md).
Every example still defaults to `http://127.0.0.1:8787` and a device
token at `~/.config/terva-lampi/token`, so a local lake works without
a hostname in this tree. On a machine that should upload to the VPS,
set `LAMPI_SERVER` to that host's HTTPS URL in the env file or the
launchd plist. Do not put the production hostname in git, and do not
point the agent at plain HTTP on a public interface.

The agent reads the URL from `--server`, then `LAMPI_SERVER`, then
`config.json`, then the loopback default. The token file is `--token-file`,
then `LAMPI_TOKEN_FILE`, then the path in `config.json`, then
`~/.config/terva-lampi/token`. The token is not a command argument.
Restart the agent to reload any of these.

## systemd user service

```text
deploy/systemd/terva-lampi-agent.service
deploy/systemd/agent.env.example
```

Copy the unit to `~/.config/systemd/user/terva-lampi-agent.service`.
Copy the env example to `~/.config/terva-lampi/agent.env` (mode 0600)
when you want to override the loopback URL or the token path. The unit
starts `%h/.local/bin/terva-lampi`. Put the binary there, or edit
`ExecStart`.

```bash
systemctl --user daemon-reload
systemctl --user enable --now terva-lampi-agent.service
```

`SIGUSR1` asks the running agent to sync. The watch on
`$TERVA_HOME/sessions` is still the source of truth.

## systemd system service

```text
deploy/systemd/terva-lampi-serve.service
deploy/systemd/serve.env.example
```

This unit runs `terva-lampi serve` on the VPS. It is a system service,
so the lake can start at boot. The agent unit above is a user service.
Copy the serve unit to `/etc/systemd/system/terva-lampi-serve.service`.
It binds `127.0.0.1:8787` and passes `--data` and `--token-file`.
`terva-lampi serve` does not read `LAMPI_SERVER` or `LAMPI_TOKEN_FILE`.
Those are agent settings. systemd substitutes `LAMPI_SERVE_ADDR`,
`LAMPI_SERVE_DATA`, and `LAMPI_SERVE_TOKEN_FILE` from the unit and,
when the file exists, from `/etc/terva-lampi/serve.env`.

The ordered checklist is [docs/vps-bringup.md](../docs/vps-bringup.md).
Create the `terva-lampi` user and the data directory before enabling
the unit. `make build` does not install it.

```bash
systemctl daemon-reload
systemctl enable --now terva-lampi-serve.service
```

## launchd agent

```text
deploy/launchd/sh.terva.lampi.agent.plist
```

Copy it to `~/Library/LaunchAgents/` and set `LAMPI_SERVER` to the VPS
HTTPS URL from [docs/policy.md](../docs/policy.md). The program path is
`$HOME/.local/bin/terva-lampi`. The token file defaults to
`$HOME/.config/terva-lampi/token`. The plist keeps the loopback URL
until you edit it.

```bash
launchctl bootstrap gui/$UID ~/Library/LaunchAgents/sh.terva.lampi.agent.plist
```

## Optional `lampi` name

The primary command is `terva-lampi`.
[neurobin/lampi](https://github.com/neurobin/lampi) already uses the bare
name. `deploy/install-lampi-alias.sh` creates a symlink, by default
`~/.local/bin/lampi`. If `lampi` already exists, the script leaves it
alone. A shell script, or a file that mentions neurobin, gets a warning
that names that installer. Invoking this program as `lampi` prints the
same warning itself.

```bash
deploy/install-lampi-alias.sh --bin "$PWD/bin/terva-lampi"
```

## Hook

`hooks/terva-post-tool-enqueue.sh` is a supported optional acceleration
for terva `post_tool_use`. `make build` does not install it. Wire it
when you want a running agent to sync at the end of a tool call
instead of waiting for the next filesystem event.

The filesystem watch remains the source of truth. The script does not
upload, and it does not wait for a sync to finish. If it never runs,
the watch still uploads while `terva-lampi agent` is up. If the agent
is down, the bytes wait on disk and the next agent start syncs them.
A nudge can also land before terva has flushed the new session lines.
The watch uploads that write when it lands.

### What the script does

On Unix, a running agent writes `agent.pid` in the state directory and
treats `SIGUSR1` as "sync now". The state directory is
`$XDG_STATE_HOME/terva-lampi/`, or `~/.local/state/terva-lampi/` when
that variable is unset. The script reads the pid and sends the signal
only when that process is the `terva-lampi` executable.

On Linux the check is `/proc/<pid>/exe`, including when the binary was
replaced and the link reads `terva-lampi (deleted)`. An agent started
through the optional `lampi` symlink is recognized when that symlink
points at `terva-lampi`. A process whose arguments only mention the
name is not signalled. On other Unix there is no `/proc`, so the
invoked command's basename must be `terva-lampi`, which is how the
launchd unit starts the agent. The `lampi` name is not treated as the
agent there.

There is no Windows hook. `SIGUSR1` is the Unix nudge. The watch is
the path on every platform.

Every path exits 0: missing pid file, a pid file that is not a number,
a pid that is not running, a pid that is not `terva-lampi`, a signal
that cannot be delivered, and a signal that was sent. A missing agent
does not fail the tool call. The script writes a one-line note on
stderr. terva ignores post-hook stdout and records a post hook only
when it exits non-zero or times out, so a quiet miss stays out of the
session. Run the script yourself when you want to see the note:

```bash
sh hooks/terva-post-tool-enqueue.sh
```

The hook inherits the environment of the terva process, not the
agent's systemd `EnvironmentFile`. `XDG_STATE_HOME` has to be the one
the agent used when it wrote `agent.pid`. If they differ, the script
does not see the pid file, exits 0, and the watch still uploads.

### Wiring

Put the hook in the terva **user** config, `$TERVA_HOME/config.json`.
When `TERVA_HOME` is unset, that file is `~/.local/state/terva/config.json`
on Linux and `~/Library/Application Support/terva/config.json` on
macOS. A project's `.terva/config.json` can add hooks only in a
trusted workspace, and those hooks are appended to yours. The user
file is the one that runs for every session.

`command` is an absolute path. terva does not expand `~`. Copying the
script off this checkout is optional; do it when the hook should keep
working after the tree is gone.

```bash
install -m 0755 hooks/terva-post-tool-enqueue.sh ~/.local/bin/terva-post-tool-enqueue.sh
```

Add a `post_tool_use` entry beside whatever else the file already
holds. Leave `tools` unset to nudge after every tool. The script does
not read the tool event. terva's default post-hook timeout is 30
seconds; this script returns immediately, so you do not need
`timeout_ms`.

```json
{
  "hooks": {
    "post_tool_use": [
      {
        "command": "/home/me/.local/bin/terva-post-tool-enqueue.sh"
      }
    ]
  }
}
```

Start terva again after editing the file. Restart `terva-lampi agent`
when you change the lake URL, the token path, or the allowlist. The
hook does not reload those.
