# Registration and lakes

Read this to add a machine to a lake, to send one machine's sessions to
more than one lake, or to understand what a lake's base configuration
can change on a machine. The lake host's side of the same steps is in
[vps-bringup.md](vps-bringup.md#devices). Back to the
[documentation index](README.md).

## Registering a machine

Registration is the usual way to add a machine. It uses a one-time
code from the lake, and the device token it makes never leaves the
machine.

The lake needs a token file before it can register devices. One
operator token is enough:

```bash
terva-lampi login --token-file ./tokens/operator.token
terva-lampi serve --token-file ./tokens &
```

On the lake host, once, record the URL agents reach the lake at:

```bash
terva-lampi serve identity set-url https://lake.example
```

Then mint one code per machine. The code is a secret. Move it the way
you would move a password, not in a chat log or a command line.

```bash
terva-lampi serve register --name laptop > laptop.code
terva-lampi serve identity          # note the key fingerprint
```

On the machine, fresh or already running an agent:

```bash
terva-lampi register --code-file laptop.code --install-service
```

A machine without the binary can install and register in one step.
`--register` runs the same command after the install. The code is read
from the terminal, because under `curl | sh` the script itself is on
stdin:

```bash
curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | sh -s -- --register
```

To type nothing on the machine, put the code and the fingerprint that
`serve identity` prints on the line. It then needs no terminal, so it
also works as `ssh host '…'`. The leading space keeps the line out of
history in shells that ignore such lines (bash with `ignorespace`, zsh
with `HIST_IGNORE_SPACE`):

```bash
 curl -fsSL https://raw.githubusercontent.com/terva-sh/lampi/main/install.sh | TERVA_LAMPI_CODE='…' sh -s -- --register --fingerprint SHA256:…
```

The code works once, so after registration the copy in history is
spent. Give such a code a short life, for example `serve register
--name laptop --expires 1h`. Without `--fingerprint`, the installer
asks you to confirm the lake on the terminal.

### What register checks and writes

`register` checks the code's signature and expiry, that the URL is
https, and that the key list at that URL holds the code's key. It then
shows the lake's URL, id, and key fingerprint. Compare the fingerprint
with the one `serve identity` printed, as you would an SSH host key,
and confirm. Without a terminal, pass `--fingerprint SHA256:…` instead.

Then it makes a device token that never leaves the machine, redeems the
code, and writes:

- `tokens/<name>.token`;
- the lake entry in `config.json`, with its pinned lake id and key;
- the lake's base configuration.

A running agent picks up the lake at once on Unix. On Windows, restart
the agent. On a fresh machine, `--install-service` writes and starts
the systemd user unit or launchd agent. Adding a second lake is the
same command with a code from that lake.

A code works once, for 24 hours unless `--expires` says otherwise.
`serve register --list` shows each code's state, who minted it
(`created_by`) and who revoked it (`revoked_by`): `cli` for
`serve register`. `--revoke` stops one.

### A token file by hand

The manual path still works and stays the fallback. `login` writes a
device token and does not print it:

```bash
terva-lampi login
terva-lampi serve --token-file ~/.config/terva-lampi/token
```

Copy that file to the lake host and pass the copy to `serve`. `serve`
stores a SHA-256 of each device token and rewrites that copy, so keep
the original as the client's secret. The token file format, the
`<name>.token` directory layout, SIGHUP reload, and device binding are
in [A token file by hand](vps-bringup.md#a-token-file-by-hand-the-fallback).
A machine added this way has no pinned lake key and gets no base
configuration.

### Tokens and plain HTTP

With `--token-file`, `/v1` routes require `Authorization: Bearer`.
`/healthz` stays open and returns no catalog data. The token is never
a command argument.

Without a token file, `serve` accepts unauthenticated requests only on
a loopback address and refuses any other `--addr`. With one, a
non-loopback `--addr` is a stderr warning: `serve` speaks plain HTTP,
so TLS belongs in front. Clients refuse to send a token to an
`http://` URL unless the host is `localhost`, 127.0.0.0/8, or `::1`.

## Many lakes

`config.json` can name more lakes under `lakes`, keyed by a short name:
lowercase letters, digits, `-`, and `_`, at most 32 characters. Each
entry has its own `server`, `token_file`, and `projects`. Registration
also writes the lake's pinned `lake_id`, `key_id`, and `public_key`.

```json
{
  "server": "https://home.example",
  "projects": {
    "allow": [{"cwd_prefix": "/home/you/src"}],
    "deny": [{"cwd_prefix": "/home/you/src/private"}]
  },
  "lakes": {
    "work": {
      "server": "https://work.example",
      "token_file": "/home/you/.config/terva-lampi/tokens/work.token",
      "projects": {"allow": [{"git_remote": "git@git.example:work/app.git"}]}
    }
  }
}
```

### How the fields combine

- The top-level `server` and `token_file`, `LAMPI_SERVER`, and
  `LAMPI_TOKEN_FILE` describe the lake named `default`, exactly as they
  did before the map. A config with no `lakes` is that one lake.
- A lake's `projects.allow` admits sessions to that lake only. The
  top-level `projects.allow` belongs to the `default` lake. With only a
  `lakes` map it is refused, so move each rule under its lake.
- Top-level `projects.deny` applies to every lake, as well as each
  lake's own deny rules. `redaction` applies to every lake.
- `token_file` defaults to `tokens/<name>.token` in the config
  directory, and to the usual `token` for an entry named `default`. An
  entry named `default` beside a top-level `server` or `token_file` is
  an error.
- `--lake NAME` picks one lake on `sync`, `status`, and `conflicts`.
  `--server` and `--token-file` then override that lake's values. With
  more than one lake they need `--lake`.
- `terva-lampi agent config` prints one `lake` line per lake, with the
  source of its server and token file.

### A machine with no lake

`"lakes": {}` with no top-level `server`, `token_file`, or
`LAMPI_SERVER` is no lake at all. The agent then discovers and
watches, uploads nothing, and says so once at start. `status` prints
`lakes: none configured`, and `sync` fails and says why. This is the
standalone state a machine is in until it is registered. A
`config.json` with no `lakes` key still means the loopback lake.

### Base configuration from a lake

A lake pinned in `config.json` (`lake_id`, `key_id`, `public_key`,
which registration writes) can publish a base configuration, its
profile. The agent fetches it at start and every hour. It checks the
copy against the pinned key and lake id, and against the entry's
`device_id` when it has one. It keeps the last copy that passed in
`lakes/<name>/profile.json`. A fetch that fails, or a copy signed by
another key, is logged once, and the cached copy stays in use. `sync`
uses the cached copy and does not fetch. A lake with no pin gets no
profile.

A profile can set harnesses on or off, the debounce, and the lake's own
`projects.allow` and `projects.deny`. It cannot set a harness root or
`redaction.upload_hits`. A lake can therefore narrow what a machine
sends, but can only widen the allowlist for uploads to itself.

`config.json` wins over every field:

- A lake's allow rules apply only when `config.json` gives that lake
  none, and its deny rules are added to the local ones.
- For the machine-wide fields, the first lake in order (`default`, then
  by name) that sets a field wins.
- A changed profile restarts that lake's push loop. A change to
  harnesses or the debounce is logged and waits for a restart.

`agent config` prints a `profile=` line per lake, `source=local`,
`source=lake:NAME`, or `source=default` for each machine-wide value, and
`allow_source=` on each lake line. The lake operator's side is
[Profiles](vps-bringup.md#profiles).

### Reloading

On Unix, SIGHUP makes a running agent read its lakes again. A lake that
was removed, or whose server, token, machine id, or rules changed,
drains its outbox before it stops, so nothing already queued for it is
dropped. A changed lake then starts again with a full pass. A lake that
did not change keeps running. A config that does not resolve leaves the
lakes as they were and says why.

The agent prints one `reload:` line naming what it added, removed,
restarted, and kept. Commands that change the lakes send the signal
through `agent.pid`. Windows has no SIGHUP, so restart the agent there.

### Per-lake state

Each lake keeps its own sync state in `lakes/<name>/` in the state
directory: its outbox, its watermarks, and its last sync and attempt
records. The quarantine records and `agent.pid` stay at the top and are
shared.

Each lake also has its own machine id. The `default` lake keeps
`machine.json`, and any other lake gets `machines/<name>.json` in the
config directory, so two lakes cannot join their data by machine.

`sync` pushes to every lake in turn, or to the one `--lake` names. The
agent pushes to every lake. Each lake has its own outbox, backoff,
debounce ceiling, and 401 message, so a lake that is down or refuses
the token waits out its own retry while the others keep receiving.

With more than one lake, each output line starts with `lake <name>: `,
and `status` prints one block per lake. A lake that cannot be prepared,
such as one whose token file cannot be read, is named on stderr and the
others still run. `sync` and `status` go on to the next lake and exit
non-zero, and the agent starts without it.

### Upgrading from a single lake

The first `sync` or agent start on a release with many lakes moves the
single-lake files into `lakes/default/` and names the new place. The
move copies the SQLite stores with `VACUUM INTO` into a hidden
directory and renames it into place, then removes the old files by
name. A lake sharing the directory (`serve` without `--data`) keeps its
`catalog.db`, `cas/`, and `identity.json`. `sync` refuses the move
while an agent from an earlier release holds `agent.pid`, so stop that
agent first.
