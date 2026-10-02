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
| `terva-lampi serve backup` | Copy the catalog (`VACUUM INTO`), the CAS, `identity.json`, and the token file to `--out`. Runs while `serve` runs. With `--archive FILE --recipient age1…` (or `--recipients-file`), write the same set to one age-encrypted archive instead. |
| `terva-lampi serve restore` | Decrypt an archive from `serve backup --archive` with `--identity-file` into a new or empty `--data` directory, then check it as `serve fsck` does. Removes what it wrote on failure. |
| `terva-lampi serve fsck` | Re-hash every CAS object and name the bad ones, and check that every session is in a bay and every bay reference names a bay that exists. `--repair` removes bad objects, with `serve` stopped; it does not change bays. |
| `terva-lampi serve devices` | List the lake's devices, or `revoke`, `unbind`, or `set-profile` one by name. Runs while `serve` runs. A revoke takes effect on the next request. |
| `terva-lampi serve bays` | List bays, or `create`, `rename`, `alias`, `unalias`, `delete` one, turn the default bay `on` or `off`, `grants`, `grant` or `revoke` read and write on a bay for an IdP group, a device or a read token, list, add (`rule`) or remove (`unrule`) the lake's hold, add and deny routing `rules`, list `holds` or `release` a held session, list the `inbox` with a reason for each session, `move` sessions between bays by filter, and `apply-rules` to stored sessions; `move` and `apply-rules` take `--dry-run`. Runs while `serve` runs. A rename keeps the old name as an alias. A delete moves sessions left in no other bay to the default and deletes no data. See [policy.md](policy.md#bays) and [bays-inbox.md](bays-inbox.md). |
| `terva-lampi serve identity` | Print the lake id, public URL, and each signing key's fingerprint. `set-url URL` records the URL agents reach the lake at. `rotate` adds a key and `retire KEY-ID` ends one; see [Rotating and retiring keys](policy.md#rotating-and-retiring-keys). Runs while `serve` runs. |
| `terva-lampi serve register` | Mint a one-time registration code for a new machine (`--name`, `--expires`, `--profile`), or `--list` and `--revoke` them. |
| `terva-lampi serve normalize` | Queue sessions to be normalized again: `--all`, `--stale` (the dashboard's unknown), `--failed`, or `--session UID`. `--all` rewrites every derived file after an upgrade changes how they are written. `--status` queues nothing and prints sessions by state, the jobs outstanding (queued or running) and how long ago the oldest was queued, and each failed session with its message, from the catalog, with or without `serve` running; `--json` prints the `/v1/stats` normalization object. `--dry-run` lists them. Runs while `serve` runs; serve starts the jobs on SIGHUP or at its next start. |
| `terva-lampi serve healthcheck` | Exit 0 when a running lake answers `GET /healthz` with `{"status":"ok"}`, and 1 with one line on stderr otherwise. `--addr` takes the value given to `serve --addr`; an unspecified host such as `0.0.0.0` is probed on loopback. `--timeout` defaults to 3s. It reads no token, config, or lake directory, uses no proxy, and does not retry. It is meant for a container `HEALTHCHECK` in an image with no shell. The listener opens only after the catalog is migrated, so the probe fails during an upgrade's migration. |
| `terva-lampi serve migrate` | Upgrade the catalog schema without starting the listener, for an init container or a step before switching images. It takes `lake.lock`, so stop `serve` first. `serve` does the same at every start. Before the first step it copies a catalog that holds data to `migration-backups/` in the lake directory, keeps the newest three copies, and stops if the copy fails. It prints one line per step, and on a lake with no catalog yet it succeeds and leaves the file for `serve` to create. The copy is fsynced before the first step. `--check` reads the version without the lock, beside a running `serve`, and reports up to date, pending, or newer than this binary (an error). Commands that write without the lock (`serve devices`, `register`, `profiles`, `normalize`, `compact --dry-run`) refuse a catalog on an older schema instead of migrating it under a running `serve`. To roll back, stop `serve`, copy the newest backup over `catalog.db`, and start the older version. |
| `terva-lampi serve purge` | Remove one session: its catalog rows, derived files, and the blobs no other session names. Dry run without `--yes`. `serve` stopped. |
| `terva-lampi serve compact` | Store each grown file's bytes once: fold older versions into prefix records of the newest, remove unreferenced tails and chunks, and compress objects an older release stored raw, then merge the search index's full-text segments to free the entries of deleted rows. `--dry-run` reports and writes nothing, and can run beside `serve`; otherwise `serve` stopped. |

### On each machine

| Command | What it does |
|---------|--------------|
| `terva-lampi agent` | Watch and upload until SIGTERM. Subcommands: `discover`, `machine-id`, `config`, `status`, and `refused`, which lists each project the allowlist keeps on this machine with the reason ([Allowlist](allowlist-and-redaction.md#the-project-allowlist)). See [The agent](agent.md). |
| `terva-lampi sync` | One pass: allowlist, ruleset v2, watermark, outbox, then PUT missing blobs and POST manifests. |
| `terva-lampi status` | Machine id, harnesses, outbox, watermarks, last sync and attempt, skipped files, server and token file, lake health, and catalog counts. See [What status prints](agent.md#what-status-prints). |
| `terva-lampi register` | Join a lake with a registration code, read from stdin, a prompt, or `--code-file`. See [Registering a machine](registration-and-lakes.md#registering-a-machine). `--install-service` enables the user unit. |
| `terva-lampi bays` | For each lake, the bays this device may write, and any bay config.json asks for that is not among them. |
| `terva-lampi bays which [PATH]` | For each lake, whether a session started at PATH (default: the current directory) uploads there, the bays it asks for, and the rule or `default` that named each. `--harness H` matches rules that name a harness. See [Asking for bays](registration-and-lakes.md#asking-for-bays). |
| `terva-lampi lakes` | List the lakes this machine reports to, `remove` one, or `adopt` one it already syncs to with a device token: pin its key and take its profile, keeping the device, machine id and sync state. `--allow-from profile` hands the allow rules to the profile after listing what that would stop uploading. See [Adopting a lake](registration-and-lakes.md#adopting-a-lake-a-machine-already-syncs-to). |
| `terva-lampi login` | Write `~/.config/terva-lampi/token` (mode 0600). |
| `terva-lampi quarantine` | `list` the redaction hits held on this machine, or `allow` one digest to upload with an `override` stamp. See [Quarantine](allowlist-and-redaction.md#quarantine). |
| `terva-lampi self-update` | Install the release the lake runs, capped at the newest release, checked against `checksums.txt`, and restart the agent service. `--check` exits 10, 11 or 12 when a patch, minor or major update is available. See [Upgrading an agent](../deploy/README.md#upgrading-an-agent). |

### Reading the lake

| Command | What it does |
|---------|--------------|
| `terva-lampi export` | Write normalized events as JSONL, or an allowlisted ShareGPT dataset. See [Export](#export). |
| `terva-lampi query events` | Read normalized events from a lake with a read token, with export's filters and `--fields`. See [Query a lake](#query-a-lake). |
| `terva-lampi conflicts` | List unresolved `divergent_copy` artifacts from the catalog: session, digests, and machines. `--resolved` adds resolved ones with their resolution. |

## Export

`terva-lampi export --format events`, the default, writes one
normalized event per line. `--bay`, repeated, limits either format to
the sessions in those [bays](policy.md#bays); without it export reads
every bay, as anything that reads the lake directory does.

### Select events and fields

With `--format events`, filters keep only the events that match every
filter given. They take the names and values of the
[web search filters](web-api.md#search) and select the same events: harness and
project come from the session, and the rest from the event.

| Flag | Keeps |
|------|-------|
| `--harness H` | Sessions from `terva`, `claude`, `codex`, `opencode`, `cursor`, `cursor-cli`, or `grok`. |
| `--project ID` | Sessions with this project id. |
| `--event-type T` | `message`, `tool_call`, `tool_result`, `usage`, `compaction`, `meta`, `error`, `unknown`, or `unreadable` for a line that is not an event. |
| `--actor A` | `user`, `assistant`, `system`, `tool` or `harness`. |
| `--tool NAME` | Events with this tool name, matched exactly. |
| `--tool-error B` | Tool results that failed (`true`) or succeeded (`false`). A result whose harness recorded neither matches neither. |
| `--raw-type T` | Events with this harness-native type, matched exactly. |
| `--since T`, `--until T` | Events recorded in this range, RFC 3339 or `YYYY-MM-DD` in UTC. `--since` is inclusive and `--until` exclusive; a date-only `--until` covers that whole day. An event with no recorded time matches neither. |

`--fields PATH,...` writes one JSON object per event that holds only
those paths, keyed by the path. A path is an event field
(`session_id`), one field of a nested object (`tool.name`, `model.id`),
or a key of `extra` (`extra.KEY`). A path the event lacks is `null`, so
every row has the same keys.

`--count-by PATH` writes counts instead of events: one
`{"value":V,"count":N}` line per distinct value at that path, by count
descending and then by value. It takes the paths `--fields` takes,
except the ones that hold free text, because a count of free text would
hand back the text itself: `content_text`, `content_ref`, `extra` and any
`extra.KEY`. It cannot be combined with `--fields`. A line over 16 MiB is
left out of a count.

An invalid value or an unknown path is refused before anything is
written, and the error names the flag. Filters, `--fields` and
`--count-by` work only with `--format events`.

Every tool call, with its harness, session and input:

```sh
sudo -u terva-lampi terva-lampi export --data /var/lib/terva-lampi \
  --event-type tool_call --fields harness,session_id,tool.name,content_text \
  --out tool-calls.jsonl
```

`--out` creates the file with mode 0600.

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

## Query a lake

`terva-lampi query events` reads the lake's
[event stream](web-api.md#event-stream) from any machine. It needs a read
token that holds `events:read`. The filters and `--fields` are export's
(see [Select events and fields](#select-events-and-fields)), and they
select the same events. `--count-by` counts on the lake, so only the
counts cross the network; a count past 100,000 distinct values is
refused, as is one whose distinct values total more than 32 MiB. At
least one filter is required.

| Flag | Meaning |
|------|---------|
| `--token-file FILE` | The read token. Without it, the file that `LAMPI_READ_TOKEN_FILE` names. There is no default path. |
| `--server URL` | The lake. Without it, the server of the `config.json` lake that `--lake NAME` names, or of the only lake that `config.json` lists. |
| `--out FILE` | Write here, mode 0600, and only when the stream is complete. The default is stdout. |

The command exits nonzero when the stream ends early, when the lake
reports that it stopped partway, when the row count does not match, or
when the lake sends nothing for two minutes. The lake sends a keepalive
at least every 15 seconds. The command follows no redirect, so the token
goes only to the server it was given.
Without `--out`, the rows that arrived are already on stdout. A summary
goes to stderr. The token is never printed.
[Read the lake from an agent](reading-the-lake.md) shows how to mint
the token and run a query.

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
