# Packaging examples

These files are examples. `make build` does not install them. Copy the
ones you want, and point them at the `terva-lampi` binary you built.

Phase 0 places the lake on a small VPS.
[docs/policy.md](../docs/policy.md) is that decision: TLS in front of
`serve`, device tokens, no TTL, and volume encryption or LUKS. Every
example still defaults to `http://127.0.0.1:8787` and a device token at
`~/.config/terva-lampi/token`, so a local lake works without a hostname
in this tree. On a machine that should upload to the VPS, set
`LAMPI_SERVER` to that host's HTTPS URL in the env file or the launchd
plist. Do not put the production hostname in git, and do not point the
agent at plain HTTP on a public interface.

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

`hooks/terva-post-tool-enqueue.sh` is an example terva `post_tool_use`
command. It is not installed. It reads `agent.pid` from the state
directory and sends `SIGUSR1` only when that pid's command line is
`terva-lampi`. If the agent is not running, the script exits 0. The
directory watch uploads the bytes later.
