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
to the host before pointing `--token-file` at it. There is no
enrolment API. `/healthz` stays open and returns no catalog data.

The tenant is Drew's machines. Do not point `serve` at a network you
do not control.

Example units under `deploy/` set no URL, so the agent falls back to
`http://127.0.0.1:8787` and a local lake works without a hostname in
git. On a machine that should upload to the VPS, set `LAMPI_SERVER`, or
`server` in `config.json`, to that host's HTTPS URL.
[vps-bringup.md](vps-bringup.md) is the order: encrypted disk, the
binary, the data directory, the device token, loopback `serve`, then
TLS.

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
`composer.composerHeaders` names at least one composer. The snapshot
calls `copyTrio`, then opens the copy read-only. The workspace
database uses those same two steps. The reader does not open a live
database. The merge puts matching `cursorDiskKV` rows into the
workspace document field `cursor_disk_kv`. Membership is
`allComposers[].composerId` on the ItemTable key
`composer.composerHeaders`. `composer.composerData` is
the older workspace list and is not the registry. A composer listed
only on `composer.composerData` is not merged. A missing global file
adds nothing to the document. If the copy or the open fails, the
workspace export fails. The global export itself still does not leave
the machine. The Cursor IDE pinned reader `Version` is `2`, and the
document field `harness_version` is that string. `confidence` is
`low`. The native session id stays `workspace/<id>`. `terva-lampi
export --format events` writes `session_id` as `cursor:workspace/<id>`.
A workspace database takes its cwd from `workspace.json`. A Cursor CLI export
needs an absolute `cwd` in the sibling `meta.json`. A missing file, a
relative path, or a file URI is an empty cwd, and the allowlist
refuses the export. Neither case adds a permit rule or a schema field.

Ruleset v2 still runs after the allowlist and before any request. A
hit is quarantined unless `redaction.upload_hits` is set. Leave that
false. The manifest is scanned too, and a hit there is refused
whatever `upload_hits` says. Neither gate rewrites the raw file.

## Machine inventory

These roles run `terva-lampi agent` for the multi-host proof:

| Role | Process |
|------|---------|
| laptop | `terva-lampi agent` |
| desktop | `terva-lampi agent` |
| remote/cloud box | `terva-lampi agent` |

Names stay at the role. The lake they upload to is the VPS running
`terva-lampi serve`.
