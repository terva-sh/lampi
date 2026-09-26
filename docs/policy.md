# Phase 0 policy

These are the placement and handling decisions for the MVP lake.
Drew Short (`human:sothr`) locked them. This file records them. It does
not provision a host, terminate TLS, set up a disk, or add an object
store. The operator checklist is [vps-bringup.md](vps-bringup.md).
That file does not provision a host either, and it does not name one.

## Lake host

`terva-lampi serve` runs on a small VPS with local disk. The MVP lake
is that process and that disk. It is not a home NAS, and it is not an
S3-compatible store plus an index VM.

The binary speaks HTTP. On the VPS, put TLS in front of `serve`. Do
not expose plain HTTP on a public interface. The default bind stays
`127.0.0.1:8787` for a lake on the same machine. A reachable listener
is the TLS endpoint in front of that process.

Device tokens are the ones already implemented. `terva-lampi login`
writes a 256-bit token to a mode-0600 file and does not print it. The
agent reads `--token-file`, `LAMPI_TOKEN_FILE`, or `token_file` in
`config.json`. `serve --token-file`
hashes each token with SHA-256, rewrites that copy to `sha256:<hex>`,
and requires `Authorization: Bearer` on `/v1`. Copy the client's file
to the host before pointing `--token-file` at it. That manual copy
stays as the fallback once registration lands
([Registration and many lakes](#registration-and-many-lakes)).
`/healthz` stays open and returns no catalog data.

The tenant is Drew's machines. Do not point `serve` at a network you
do not control.

Example units under `deploy/` set no URL, so the agent falls back to
`http://127.0.0.1:8787` and a local lake works without a hostname in
git. On a machine that should upload to the VPS, set `LAMPI_SERVER`, or
`server` in `config.json`, to that host's HTTPS URL.
[vps-bringup.md](vps-bringup.md) is the order: encrypted disk, the
binary, the data directory, the device token, loopback `serve`, then
TLS.

## Registration and many lakes

Phase 0 said there was no enrolment API. On 2026-09-27 Drew reversed
that and locked the model in this section. The work is tracked under
TKT-01M3FHHB (Agent onboarding: registration codes, lake config, many
lakes). Until each part lands, the manual token copy above is the way
to add a device, and it stays documented as the fallback afterwards.

### The model

- **The lake has an identity.** `serve` holds an ed25519 key list and a
  random lake id in its data directory, and records the lake id in the
  catalog. Backup and restore keep them. A new lake, or one upgrading
  from a release with no identity, gets one on its first start. A
  catalog that has recorded a lake id but lost its key does not start
  with a new one, because agents pin the key. Restore the key from a
  backup.
- **The lake publishes its keys.** `GET /.well-known/terva-lampi/keys`
  lists the lake id and each key with its status and validity window.
  The response is signed over a nonce the caller sends.
- **Devices have names.** Each token belongs to a named device with an
  id. The lake records which device made each request and can list and
  revoke devices by name. A device binds to one `machine_id`. A token
  from the old token file binds to the first `machine_id` it uploads
  under after the upgrade, and `serve devices unbind` resets that.
- **Registration codes.** The operator mints a code on the lake host.
  The code holds the lake URL, the lake id, the key that signed it, a
  one-time secret, an expiry (24 hours by default), and the signature.
  It does not hold a device token. The agent makes its own token and
  sends only its SHA-256 when it redeems the code at `/v1/register`.
  A code redeems once.
- **Base configuration.** A lake can publish a signed profile with
  `harnesses`, debounce values, `redaction`, and `projects` rules. The
  local `config.json` wins over every field. A local deny wins over a
  lake's allow. A lake's rules apply only to uploads to that lake.
- **Many lakes.** An agent can report to several lakes. Each lake has its
  own token, allowlist, sync state and `machine_id`, so two lakes cannot
  join their data by machine. Top-level deny rules and redaction apply to
  every lake.
- **Entry.** The code is a secret. It is read from stdin, a prompt, or a
  file, and never from a command argument.

### Routes without a token

Three routes answer without a token, and none returns catalog data.

| Route | Exposes |
|------|---------|
| `GET /healthz` | That `serve` is up |
| `GET /.well-known/terva-lampi/keys` | The lake id and its public keys |
| `POST /v1/register` | Whether a one-time secret is valid, and then the new device's id and base configuration |

Rate-limit the last two at the proxy and in `serve`. A failed
redemption is logged without the secret.

### What each piece protects

| If this leaks or is forged | The holder can | Bounded by |
|------|---------|---------|
| A registration code | Register one device, once, before it expires | Single use, the expiry, `serve register --revoke` |
| A device token | Upload as that device | `serve devices revoke` |
| A copied key list | Nothing new; it cannot sign a fresh nonce | The nonce signature |
| A code with the lake's URL and another key | Nothing; `register` refuses it | The key list fetched over TLS from that URL |
| A code signed by a retired key | Nothing; the lake and `register` refuse it | Key status |
| A forged code with the attacker's own URL | Receive the sessions that machine's allowlist admits | Only the fingerprint check |

The last row is the gap. Before it redeems a code, `register` shows the
URL, the lake id, and the key fingerprint, and asks for confirmation.
`serve identity` prints the same fingerprint on the lake host. Compare
the two, as you would an SSH host key. A run with no terminal must be
given the fingerprint.

After registration, the agent checks the pinned key on every `hello` and
on every base configuration it fetches. It refuses a lake at the same
URL whose key does not chain to the pin.

### Audit

The lake appends to an audit log in its data directory for these events:
creating, redeeming, expiring and revoking a code; creating, binding,
unbinding and revoking a device; adding and retiring a key; and every
refused redemption. Backup covers the log. It never holds a secret or a
token.

### Upgrade order

Upgrade the lake before any agent. The new routes and `hello` fields are
additive, so `capture_protocol` stays 1 and an old agent keeps working.
A new agent that finds no key endpoint still syncs to a lake set by
`server` and `token_file`. `register` refuses that lake and says to
upgrade it.

### Windows

Windows has no SIGHUP. On Windows, adding or removing a lake takes
effect when the agent restarts.

## Retention

No TTL. Session bytes, catalog rows, and normalized projections stay
until `terva-lampi serve purge --session <uid> --yes` removes that
session, with `serve` stopped. Purge keeps a blob another session
names. A backup taken earlier still holds the bytes. This tree does
not delete by age.

## Encryption at rest

Require the provider's volume encryption and/or LUKS on the VPS data
disk. CAS objects and `catalog.db` sit on that volume. There is no
application-level age wrapping for the MVP.

## Off-box raw

Allowlisted projects only. The gate is `projects` in `config.json`,
implemented in `internal/config`. This policy confirms that surface.

- Default deny. An empty `projects.allow` refuses every project.
- `projects.deny` wins over allow.
- A rule matches a cwd prefix on a path boundary, a git remote, or
  terva's cwd hash (`hex(sha256(cwd)[:8])`). Every field set on the
  rule has to match. A rule with no fields matches nothing.
- A deny rule reads a doubt as a match. `cwd_prefix` ignores case
  and is checked with and without symlinks resolved. `cwd_hash` also
  matches the resolved cwd. `git_remote` also matches a session whose
  remote cannot be read. A cwd outside any repository has no remote
  and does not match it. Allow rules compare exactly.
- Git remotes are folded before comparison, so the scp and https
  spellings of one remote are one key. Only the remote named origin
  is copied onto the manifest, and only when the session cwd still
  has a `.git`. A URL remote loses its user part and password, except
  that an ssh URL keeps a bare login name.
- Project resolution reads files. `git` runs only for an admitted
  session whose root commit the reader cannot find, with config
  pinned so the checkout cannot make it run a program.
- The allowlist hash is not the lake's project id. `project_id` is
  the normalized origin URL and the repository root commit. See
  [protocol.md](protocol.md).

Cursor IDE and Cursor CLI exports use that same gate. The global IDE
database has an empty cwd and is refused by design. Current Cursor
builds keep chat bodies in the global database's `cursorDiskKV` table,
so a read of the workspace database alone misses type 1 and type 2
bubbles. The export copies the global database when
`composer.composerHeaders` names at least one composer. One sync
copies it at most once and shares that copy across workspaces. Each
workspace selects, in SQL, only the `cursorDiskKV` rows of the
composers it names. A snapshot copies the database and its WAL, not
the `-shm` index. When a checkpoint moved the files during the copy,
it copies again, up to five times. `PRAGMA quick_check` must pass on
the copy, which is then opened read-only. The workspace database uses
the same snapshot. The reader does not open a live database. The merge
puts matching `cursorDiskKV` rows into the workspace document field
`cursor_disk_kv`. Membership is `allComposers[].composerId` on the
ItemTable key `composer.composerHeaders`. `composer.composerData` is
the older workspace list and is not the registry. A composer listed
only on `composer.composerData` is not merged. A missing global file
adds nothing to the document. If the copy or the open fails, the
workspace export fails. The global export itself still does not leave
the machine. `sync` asks the allowlist before it builds an export, so
the global database and a refused workspace are not copied or exported
at all. `sync` still names them as refused. The Cursor IDE pinned
reader `Version` is `2`, and the document field `harness_version` is
that string. `confidence` is `low`. The native session id stays
`workspace/<id>`. `terva-lampi export --format events` writes
`session_id` as `cursor:workspace/<id>`. A workspace database takes
its cwd from `workspace.json`. A Cursor CLI export needs an absolute
`cwd` in the sibling `meta.json`. A missing file, a relative path, or
a file URI is an empty cwd, and the allowlist refuses the export.
Neither case adds a permit rule or a schema field.

Ruleset v2 still runs after the allowlist and before any request. A
hit is quarantined unless `redaction.upload_hits` is set, or unless
`terva-lampi quarantine allow` acknowledged that file's exact digest.
Leave `upload_hits` false. The manifest is scanned too, and a hit there is refused
whatever `upload_hits` says. Neither gate rewrites the raw file.

## Machine inventory

These roles run `terva-lampi agent` for the multi-host proof:

| Role | Process |
|------|---------|
| laptop | `terva-lampi agent` |
| desktop | `terva-lampi agent` |
| remote/cloud box | `terva-lampi agent` |

Names stay at the role. The lake they upload to is the VPS running
`terva-lampi serve`. An agent can also report to more lakes; see
[Registration and many lakes](#registration-and-many-lakes).
