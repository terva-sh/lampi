# Command reference

Every `terva-lampi` command, the export formats, and the order in which
a setting is resolved. `terva-lampi --help` lists the commands, and
`terva-lampi <command> --help` prints the flags. Back to the
[documentation index](README.md).

## Commands

### On the lake host

| Command | What it does |
|---------|--------------|
| `terva-lampi serve` | Lake. `GET /healthz`, `GET /v1/stats`, `GET /v1/conflicts`, blob check and put, manifests, and the dashboard when `--web-config` is set. `--metrics-addr` adds a loopback Prometheus listener. `--behind-proxy` says TLS terminates in a proxy in front, as in a container on a private network: a non-loopback `--addr` then logs one line instead of the plaintext warning. It needs `--token-file` with at least one token. |
| `terva-lampi serve backup` | Copy the catalog (`VACUUM INTO`), the CAS, `identity.json`, and the token file to `--out`. Runs while `serve` runs. |
| `terva-lampi serve fsck` | Re-hash every CAS object and name the bad ones. `--repair` removes them, with `serve` stopped. |
| `terva-lampi serve devices` | List the lake's devices, or `revoke`, `unbind`, or `set-profile` one by name. Runs while `serve` runs. A revoke takes effect on the next request. |
| `terva-lampi serve identity` | Print the lake id, public URL, and each signing key's fingerprint. `set-url URL` records the URL agents reach the lake at. `rotate` adds a key and `retire KEY-ID` ends one; see [Rotating and retiring keys](policy.md#rotating-and-retiring-keys). Runs while `serve` runs. |
| `terva-lampi serve register` | Mint a one-time registration code for a new machine (`--name`, `--expires`, `--profile`), or `--list` and `--revoke` them. |
| `terva-lampi serve normalize` | Queue sessions to be normalized again: `--all`, `--stale` (the dashboard's unknown), `--failed`, or `--session UID`. `--all` rewrites every derived file after an upgrade changes how they are written. `--status` queues nothing and prints sessions by state, the jobs outstanding (queued or running) and how long ago the oldest was queued, and each failed session with its message, from the catalog, with or without `serve` running; `--json` prints the `/v1/stats` normalization object. `--dry-run` lists them. Runs while `serve` runs; serve starts the jobs on SIGHUP or at its next start. |
| `terva-lampi serve healthcheck` | Exit 0 when a running lake answers `GET /healthz` with `{"status":"ok"}`, and 1 with one line on stderr otherwise. `--addr` takes the value given to `serve --addr`; an unspecified host such as `0.0.0.0` is probed on loopback. `--timeout` defaults to 3s. It reads no token, config, or lake directory, uses no proxy, and does not retry. It is meant for a container `HEALTHCHECK` in an image with no shell. The listener opens only after the catalog is migrated, so the probe fails during an upgrade's migration. |
| `terva-lampi serve migrate` | Upgrade the catalog schema without starting the listener, for an init container or a step before switching images. It takes `lake.lock`, so stop `serve` first. `serve` does the same at every start. Before the first step it copies a catalog that holds data to `migration-backups/` in the lake directory, keeps the newest three copies, and stops if the copy fails. It prints one line per step. `--check` reads the version without the lock, beside a running `serve`, and reports up to date, pending, or newer than this binary (an error). Commands that write without the lock (`serve devices`, `register`, `profiles`, `normalize`, `compact --dry-run`) refuse a catalog on an older schema instead of migrating it under a running `serve`. To roll back, stop `serve`, copy the newest backup over `catalog.db`, and start the older version. |
| `terva-lampi serve purge` | Remove one session: its catalog rows, derived files, and the blobs no other session names. Dry run without `--yes`. `serve` stopped. |
| `terva-lampi serve compact` | Store each grown file's bytes once: fold older versions into prefix records of the newest, and remove unreferenced tails and chunks. `--dry-run` reports and writes nothing, and can run beside `serve`; otherwise `serve` stopped. |

### On each machine

| Command | What it does |
|---------|--------------|
| `terva-lampi agent` | Watch and upload until SIGTERM. Subcommands: `discover`, `machine-id`, `config`, `status`, and `refused`, which lists each project the allowlist keeps on this machine with the reason ([Allowlist](allowlist-and-redaction.md#the-project-allowlist)). See [The agent](agent.md). |
| `terva-lampi sync` | One pass: allowlist, ruleset v2, watermark, outbox, then PUT missing blobs and POST manifests. |
| `terva-lampi status` | Machine id, harnesses, outbox, watermarks, last sync and attempt, skipped files, server and token file, lake health, and catalog counts. See [What status prints](agent.md#what-status-prints). |
| `terva-lampi register` | Join a lake with a registration code, read from stdin, a prompt, or `--code-file`. See [Registering a machine](registration-and-lakes.md#registering-a-machine). `--install-service` enables the user unit. |
| `terva-lampi lakes` | List the lakes this machine reports to, or `remove` one. |
| `terva-lampi login` | Write `~/.config/terva-lampi/token` (mode 0600). |
| `terva-lampi quarantine` | `list` the redaction hits held on this machine, or `allow` one digest to upload with an `override` stamp. See [Quarantine](allowlist-and-redaction.md#quarantine). |

### Reading the lake

| Command | What it does |
|---------|--------------|
| `terva-lampi export` | Write normalized events as JSONL, or an allowlisted ShareGPT dataset. See [Export](#export). |
| `terva-lampi conflicts` | List `divergent_copy` artifacts from the catalog: session, digests, and machines. |

## Export

`terva-lampi export --format events`, the default, writes one
normalized event per line.

`--format sharegpt` and `--format trajectory` write one ShareGPT
conversation per session that `config.json` allowlists and that has a
training turn. A session with no training turn, and a session that is
not permitted, are named on stderr and left out.

- Each training row carries `raw_sha256`, the current transcript blob.
- `encrypted_content` is copied onto the turn as stored and is not
  decrypted.
- Ruleset v2 strips matches from the plaintext training fields
  (`value`, tool name, and call id). The command does not rewrite the
  CAS or the normalized events, and `--format events` is not stripped.

While `serve` runs on the same `--data`, export reads the catalog
read-only and starts no normalize worker. A session that `serve` has
not normalized yet is named on stderr and left out.

## Where a setting comes from

`agent`, `sync`, `status`, `agent config`, and `conflicts` resolve the
server URL and the token file in this order:

1. The flag (`--server`, `--token-file`).
2. The environment (`LAMPI_SERVER`, `LAMPI_TOKEN_FILE`).
3. `config.json`.
4. The default: `http://127.0.0.1:8787` and `~/.config/terva-lampi/token`.

`status` and `agent config` print `source=flag`, `env`, `config`, or
`default` on the `server` and `token_file` lines. `serve` reads only
`--token-file`.

A harness `root` uses a different order:

1. The flag, if the command has one.
2. The `harnesses` entry in `config.json`.
3. The harness environment variable, such as `CLAUDE_CONFIG_DIR`. This
   is a debug override.
4. The adapter default.

Restart the agent after you edit the `harnesses` map. The fields are in
[Harnesses in deploy/README.md](../deploy/README.md#harnesses).

## Files on a machine

| File | Where | What it holds |
|------|-------|---------------|
| `config.json` | `~/.config/terva-lampi/` (`$XDG_CONFIG_HOME/terva-lampi/` when set) | Lakes, allowlist, harnesses, debounce, redaction |
| `token` | the config directory | The device token for the `default` lake |
| `tokens/<name>.token` | the config directory | Device tokens for other lakes |
| `machine.json` | the config directory | The machine id: a ULID created once. It is not a fleet origin |
| `machines/<name>.json` | the config directory | The machine id for a lake other than `default` |
| `lakes/<name>/` | the XDG state directory, `terva-lampi/` | Outbox, watermarks, last sync, cached profile |
| `quarantine.jsonl`, `agent.pid` | the state directory | Redaction hits and the running agent's pid, shared by every lake |
