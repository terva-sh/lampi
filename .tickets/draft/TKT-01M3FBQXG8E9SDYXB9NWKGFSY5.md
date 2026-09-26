---
schema: 3
id: TKT-01M3FBQXG8E9SDYXB9NWKGFSY5
title: Add optional compressed age-encrypted backups and restore
type: task
status: draft
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
claim: null
archive: null
created_at: 2026-09-26T17:20:55Z
updated_at: 2026-09-26T17:20:55Z
created_by:
  id: agent:codex/deploy
  name: ""
updated_by:
  id: agent:codex/deploy
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

- [ ] Document the archive, compression, age recipient/identity and compatibility design before implementation.
- [ ] Optional encrypted backups restore successfully with bounded resources, private permissions and no credential leakage.
- [ ] Failure and interruption tests cover archive publication, integrity, wrong keys and temporary plaintext cleanup.
