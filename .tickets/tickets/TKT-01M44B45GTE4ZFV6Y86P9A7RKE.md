---
schema: 3
id: TKT-01M44B45GTE4ZFV6Y86P9A7RKE
title: Cursor CLI export uploads the session's blobEncryptionKey
type: bug
status: in-progress
status_reason: null
priority: high
due_on: null
labels:
  - area/adapter
  - area/redact
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/580cbe08
  branch: cursor/drop-blob-key
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-580cbe08
  commit: 783cc0c1746f3877447b1ce0eb29e4dd2d7dbd3f
  session: null
  claimed_at: 2026-10-04T20:54:23Z
  expires_at: null
archive: null
created_at: 2026-10-04T20:54:11Z
updated_at: 2026-10-04T20:56:11Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

The Cursor CLI export keeps the `blobEncryptionKey` field of the session record under meta key `0`. `excludedKey` in `internal/adapter/cursorcli/snapshot.go` drops `cursorAuth` keys and token field names, and nothing else. The value is a 64-character string, and the redaction ruleset has no rule that matches it, so a session that passes the allowlist uploads the key to the lake.

It was found on 2026-10-04 while looking into why Cursor sessions from a workstation did not upload. Nothing from Cursor had reached that lake: the harness was off and the three `chats/` sessions have no cwd. The ACP reader that comes next uses the same export, so the key has to be dropped before it lands.

The normalize projector keeps its own list in `cliAuthKey` (`internal/normalize/cursorcli.go`), which says it mirrors what the adapter drops. A lake that already stored an export with the key should not show the key in normalized events either.

## Acceptance criteria

- [ ] The Cursor CLI export drops blobEncryptionKey from JSON objects at any depth, ignoring case
- [ ] The normalize projector drops the same key from a stored export
- [ ] A test fails if the key reaches the export or the normalized events

## Implementation plan

Add `blobencryptionkey` to the exact-name list in `excludedKey` (adapter) and `cliAuthKey` (normalize), so the key is dropped wherever the other credential names are: a meta key, a blob id, and a JSON object key at any depth, ignoring case.

Alternatives considered:

- Drop only the field on the meta `0` record. Lost: the existing lists already apply at every depth and in both places, and a narrower rule is a second mechanism for the same job.
- A redaction rule for 64-hex strings. Lost: it would fire on every SHA-256 blob id in the export, quarantining every Cursor session, and it catches the value only by its shape, not by where it sits.
- Bump the reader `Version`. Lost: the document shape does not change, only a field is absent. Earlier credential names were added without a bump. A stored export changes digest once and re-uploads.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T20:54:17Z

Promoted from draft by the owner's instruction on 2026-10-04: file, promote and complete the three Cursor tickets in order (key fix, ACP reader, per-blob upload).

**agent:claude-code/580cbe08** at 2026-10-04T20:56:11Z

Each fix was reverted alone to check its test: the adapter revert fails TestSnapshotFiltersAuthAndReadsWAL and TestExcludedKey, and the normalize revert fails TestCursorCLIAuthAbsent. GOFLAGS=-mod=mod just ci is green. -mod=mod rewrites go.mod (blake3 direct vs indirect), which was already on main and is not part of this change; it was reverted before commit.
