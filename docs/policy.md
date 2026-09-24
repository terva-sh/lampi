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
agent reads `--token-file` or `LAMPI_TOKEN_FILE`. `serve --token-file`
hashes each token with SHA-256, rewrites that copy to `sha256:<hex>`,
and requires `Authorization: Bearer` on `/v1`. Copy the client's file
to the host before pointing `--token-file` at it. There is no
enrolment API. `/healthz` stays open and returns no catalog data.

The tenant is Drew's machines. Do not point `serve` at a network you
do not control.

Example units under `deploy/` keep `http://127.0.0.1:8787` so a local
lake works without a hostname in git. On a machine that should upload
to the VPS, set `LAMPI_SERVER` to that host's HTTPS URL.
[vps-bringup.md](vps-bringup.md) is the order: encrypted disk, the
binary, the data directory, the device token, loopback `serve`, then
TLS.

## Retention

No TTL. Session bytes, catalog rows, and normalized projections stay
until an explicit manual purge. This tree does not delete by age.

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
- Git remotes are folded before comparison, so the scp and https
  spellings of one remote are one key. Only the remote named origin
  is copied onto the manifest, and only when the session cwd still
  has a `.git`.
- The allowlist hash is not the lake's project id. `project_id` is
  the normalized origin URL and the repository root commit. See
  [protocol.md](protocol.md).

Cursor IDE and Cursor CLI exports use that same gate. The global IDE
database has an empty cwd and is refused by design: one file holds
every workspace, and the reader does not split it. A workspace
database takes its cwd from `workspace.json`. A Cursor CLI export
needs an absolute `cwd` in the sibling `meta.json`. A missing file, a
relative path, or a file URI is an empty cwd, and the allowlist
refuses the export. Neither case adds a permit rule or a schema field.

Ruleset v1 still runs after the allowlist and before any request. A
hit is quarantined unless `redaction.upload_hits` is set. Leave that
false. Neither gate rewrites the raw file.

## Machine inventory

These roles run `terva-lampi agent` for the multi-host proof:

| Role | Process |
|------|---------|
| laptop | `terva-lampi agent` |
| desktop | `terva-lampi agent` |
| remote/cloud box | `terva-lampi agent` |

Names stay at the role. The lake they upload to is the VPS running
`terva-lampi serve`.
