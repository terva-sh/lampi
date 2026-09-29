---
schema: 3
id: TKT-01M3FBQXG8E9SDYXB9NWKGFSY5
title: Add optional compressed age-encrypted backups and restore
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/ops
  - area/cas
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/d8436f9f
  branch: tickets/fbqx-design
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-d8436f9f
  commit: ec1e8486effb8d667b778defc9c0c75351d7bd88
  session: null
  claimed_at: 2026-09-29T01:39:06Z
  expires_at: null
archive: null
created_at: 2026-09-26T17:20:55Z
updated_at: 2026-09-29T01:39:06Z
created_by:
  id: agent:codex/deploy
  name: ""
updated_by:
  id: agent:claude-code/d8436f9f
  name: ""
extensions: {}
---

## Description

Provide an optional compressed, age-encrypted backup format as an alternative or fallback to storage-level encryption. The owner confirmed the existing host volume is encrypted, so this is future hardening rather than a blocker for the current dashboard upgrade. Preserve the existing directory backup/restore interface by default.

### Design questions
Choose an in-process age implementation versus invoking a separately installed tool, and a streaming archive/compression format that bounds memory. Encrypt to configured public recipients; do not require private decryption identities on the lake host. Decide how protected device-token files and separately managed OIDC configuration are included explicitly. Define interruption, corrupted archive, wrong identity, multiple-recipient and permission behavior. Protect temporary plaintext and avoid secrets in flags, output or repository files.

### Validation scope
Demonstrate an encrypted backup restored into a fresh directory with matching catalog/CAS/export content. Verify that interrupted/failed backups never publish a successful-looking archive and that failure paths clean up only temporary files created by the command. Compression must occur before encryption. Document key ownership, restore prerequisites and rotation/recovery procedures without creating or rotating real credentials.

## Acceptance criteria

- [x] Document the archive, compression, age recipient/identity and compatibility design before implementation.
- [ ] Optional encrypted backups restore successfully with bounded resources, private permissions and no credential leakage.
- [ ] Failure and interruption tests cover archive publication, integrity, wrong keys and temporary plaintext cleanup.

## Implementation plan

Proposed design, for the owner to approve before any code (acceptance criterion 1). Nothing is implemented.

### Command shape

- `serve backup --archive FILE --recipient age1… [--recipient …] [--recipients-file PATH]`
  writes one encrypted archive. Without `--archive`, `serve backup --out DIR`
  is unchanged, as the ticket requires.
- `serve restore --archive FILE --identity-file PATH --data NEWDIR` restores
  into a directory that does not exist or is empty. It refuses a lake
  directory that holds anything.

### Crypto: in-process `filippo.io/age`

- It is a pure-Go library, so the binary stays cgo-free, and the image
  needs no `age` binary. The distroless image has no shell to run one.
  v1.3.1 is already in the module cache.
- Encryption uses X25519 recipients only: `age1…` public keys. The lake
  host never holds an identity. Only `serve restore` reads one, and only
  from a file (`--identity-file`), never from a flag or the environment,
  so it does not reach shell history or `ps`.
- Several recipients are allowed, so an owner key and an offline
  escrow key can each restore.
- **Alternative considered:** running the `age` CLI. Rejected: an extra
  binary in the image, plaintext on a pipe between processes, and
  errors that are harder to report.

### Archive format

The stream is tar, then zstd, then age, in that order:
compression before encryption, as the ticket requires.

- The first tar entry is `lampi-backup.json`:
  `{format: 1, lake_id, created, lampi_version, catalog_schema}`.
  Restore reads it before anything else and refuses a format it does not
  know.
- Then the same set the directory backup copies, in the same order:
  - `catalog.db`, a VACUUM INTO snapshot
  - `cas/sha256`, then `cas/logical`, with `closeRecords`' chain-following
  - `identity.json`
  - `audit.jsonl`
  - the token file or directory, when `--token-file` is given
- CAS objects are streamed as stored. Frames are already zstd, so the
  outer zstd gains little on them and mainly packs the catalog and
  indexes.
- Memory is bounded: one file at a time through a fixed-size encoder
  window, and age streams in 64 KiB chunks.

### Publication and failure

- The archive is written to `FILE.tmp-*` in FILE's directory, mode 0600.
  It is fsynced and renamed only after the age writer closes.
- Any error, or SIGINT or SIGTERM, removes that temp file, so a failed
  run leaves no file that looks like a finished archive. Nothing else in
  the destination is touched.
- The VACUUM INTO snapshot is the one plaintext temp file. It goes in
  the lake directory, which already holds that plaintext, never in the
  destination. It is created 0600 and removed when the run ends, on
  success or failure.
- `--prune` does not apply. Each archive is a full backup, and
  retention is the operator's rotation of whole files.

### Restore checks

A wrong identity, a truncated file, a corrupt zstd stream, a tar entry
whose path leaves NEWDIR (`..`, absolute, symlink), or an unknown format
each stops the restore. On a failure, restore removes NEWDIR only if
restore created it.

After extracting, restore runs the same checks `serve fsck` does:
- the CAS re-hash
- identity.json against the catalog's lake id
- the catalog opens

Files are written 0600 and directories 0700.

### Left out

- The web config and the OIDC client secret file. They stay part of the
  separate protected-configuration backup, as `web-dashboard.md` already
  says. Putting a secret that another system issued into the lake's
  archive would widen who holds it.
- Nothing is printed or stored that names an identity. Recipients are
  public, so their fingerprints may be logged.

### Tests planned

- A round trip into a fresh directory, compared by catalog rows, CAS
  bytes, and export output.
- Each of these leaves no archive and no temp file:
  - a failure mid-stream (a writer that fails part-way)
  - SIGTERM mid-stream
  - a destination that is not writable
- Restore refuses each of: a wrong identity, a bit flipped in the
  ciphertext, a truncated archive, a path-traversal entry, and an
  unknown format.
- With two recipients, each identity restores.
- Files are 0600 and directories 0700 after a restore.

### Docs

`vps-bringup.md` and `container.md` would gain:
- key ownership: who holds the identity, where it is escrowed, and that
  the lake never holds it
- a restore drill
- recipient rotation: add the new recipient, take a new archive, retire
  the old one; old archives still need the old identity
- how TKT-01M3MC0S0's scheduled and off-host copies use archives in
  place of plaintext directories

No real credentials are created or rotated.

## Notes

**agent:claude-code/d8436f9f** at 2026-09-29T00:29:47Z

Design written as the plan and waiting for the owner's decision on the age dependency, the command shape, and what the archive leaves out, before implementation, per acceptance criterion 1. Not claimed, so it stays open to whoever implements it.

**agent:claude-code/d8436f9f** at 2026-09-29T01:39:06Z

On 2026-09-29 the owner approved the proposed design in this ticket's plan: in-process filippo.io/age, serve backup --archive with --recipient, serve restore with --identity-file, and tar, then zstd, then age. The web config and the OIDC secret are left out. Implementation starts after the open PRs land.
