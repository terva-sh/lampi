# Command reference

Every `terva-lampi` command, the export formats, and the order in which
a setting is resolved. `terva-lampi --help` lists the commands, and
`terva-lampi <command> --help` prints the flags. Back to the
[documentation index](README.md).

## Commands

### On the lake host

| Command | What it does |
|---------|--------------|
| `terva-lampi serve` | Lake. `GET /healthz`, `GET /v1/stats`, `GET /v1/conflicts`, blob check and put, manifests, and the dashboard when `--web-config` is set. |
| `terva-lampi serve backup` | Copy the catalog (`VACUUM INTO`), the CAS, `identity.json`, and the token file to `--out`. Runs while `serve` runs. |
| `terva-lampi serve fsck` | Re-hash every CAS object and name the bad ones. `--repair` removes them, with `serve` stopped. |
| `terva-lampi serve devices` | List the lake's devices, or `revoke`, `unbind`, or `set-profile` one by name. Runs while `serve` runs. A revoke takes effect on the next request. |
| `terva-lampi serve identity` | Print the lake id, public URL, and each signing key's fingerprint. `set-url URL` records the URL agents reach the lake at. `rotate` adds a key and `retire KEY-ID` ends one; see [Rotating and retiring keys](policy.md#rotating-and-retiring-keys). Runs while `serve` runs. |
| `terva-lampi serve register` | Mint a one-time registration code for a new machine (`--name`, `--expires`, `--profile`), or `--list` and `--revoke` them. |
| `terva-lampi serve purge` | Remove one session: its catalog rows, derived files, and the blobs no other session names. Dry run without `--yes`. `serve` stopped. |

### On each machine

| Command | What it does |
|---------|--------------|
| `terva-lampi agent` | Watch and upload until SIGTERM. Subcommands: `discover`, `machine-id`, `config`, `status`. See [The agent](agent.md). |
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
