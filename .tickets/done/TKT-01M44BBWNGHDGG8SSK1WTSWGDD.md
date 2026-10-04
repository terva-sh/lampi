---
schema: 3
id: TKT-01M44BBWNGHDGG8SSK1WTSWGDD
title: "Cursor CLI: skip unchanged and quarantined sessions without re-export"
type: task
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/adapter
  - area/agent
  - area/redact
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M44B45GTE4ZFV6Y86P9A7RKE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-04T20:58:24Z
updated_at: 2026-10-04T21:13:16Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

The Cursor CLI reader takes no memo (`memoized` in `internal/upload/upload.go` says so). Every pass snapshots and exports every permitted `store.db`, and only then does `unchanged` find the digest matches the watermark. A pass runs after any harness's write, which on a busy workstation is every 30 seconds or so. With the ACP sessions of TKT-01M44B45JRVQ1W2XF5H0K3MC4X (Cursor CLI: read ACP sessions under acp-sessions/), that is gigabytes copied, encoded and hashed per pass for sessions that did not change.

A quarantined session has the same problem for every harness. It never gets a watermark, so `unchanged` is false on every pass, and the file is read and scanned again to reach the same quarantine record. For a Claude JSONL that is cheap. For a multi-gigabyte Cursor export it is the full export on every pass.

Found on 2026-10-04 while planning the ACP reader. Filed separately so the ACP reader lands on a reader that is cheap when nothing changed.

## Acceptance criteria

- [x] A Cursor CLI session whose store.db and WAL stat did not change is not snapshotted or exported on a non-full pass
- [x] A memo hit that still needs bytes (no watermark, pending outbox) exports then, and the redaction scan still sees hidden bytes
- [x] An artifact whose digest is already quarantined under the current ruleset and not allowed is not read or scanned again, and is still reported as quarantined
- [x] Tests count snapshots to prove a second unchanged pass takes none

## Implementation plan

Three parts.

1. `cursorcli.ManifestsMemo` takes the upload memo. It keys on the `store.db` stat from the walk and keeps, beside the export digest, the `store.db-wal` stat taken before the snapshot, the export size, and the hidden-bytes scan. A WAL write leaves `store.db`'s stat alone, so the WAL stat has to be part of the key. On a hit the manifest carries the remembered digest and size, the hidden scan goes back into `Bundle.Hidden`, and no snapshot is taken.
2. `adapter.Bundle.Load` builds an export on demand for a manifest with a digest and no path. `readArtifacts` calls it. A memo hit that still needs bytes (no watermark, an outbox row) exports then. If the bytes changed under an unchanged stat, the digest check fails as `errFileChanged` and the new digest is remembered for the next pass.
3. `prepareBundle` reads the quarantine log's file records under the current ruleset once per run. A session whose every artifact digest has a record, with no allow entry and no `upload_hits`, is counted and reported as quarantined without being read or scanned.

Alternatives considered:

- Keep each Cursor export on disk between passes and point `Paths` at it. Lost: it doubles the disk used by multi-gigabyte sessions, and the lazy load only costs a snapshot in the rare case a hit needs bytes.
- Fold the WAL stat into a synthetic `FileStat` for the memo key. Lost: it makes the memo's stat mean something different for one harness; keeping the WAL stat in `Ident` leaves the memo contract as it is.
- Skip a quarantined session when any one artifact digest has a record. Lost: the full scan would also record the other artifacts that hit. Requiring every artifact keeps the report the same; for Cursor, which has one artifact per session, the two rules are the same.
- Cache the quarantine set across passes. Lost: the log is a few thousand lines and a run already reads the allow list the same way.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T20:58:31Z

Filed and promoted under the owner's instruction of 2026-10-04 to file, promote and complete the Cursor work in order. It sits between the key fix and the ACP reader because the ACP reader multiplies the per-pass cost.

**agent:claude-code/580cbe08** at 2026-10-04T21:04:24Z

Each part was removed alone to check its test: the quarantine skip (TestKnownQuarantineIsNotScannedAgain), the Load call (TestSyncCursorCLIMemoLoadsWhenBytesAreNeeded), the memo recall and the WAL stat check (TestManifestsMemoSkipsAnUnchangedStore). The first WAL mutation was written as 'false && a || b || c', which left b and c live, and passed; rewritten as 'false && (...)' it fails as it should. GOFLAGS=-mod=mod just ci is green.

## Summary

Landed in #190 (merge f37526e). The Cursor CLI reader takes the upload memo, keyed on store.db's stat with the WAL stat, export size and hidden-bytes scan beside the digest, and does not snapshot a session whose two stats have not moved. Bundle.Load exports a recalled session when the upload still needs the bytes. A session whose every artifact digest is already in the quarantine log under the current ruleset, with no allow entry and no upload_hits, is reported as quarantined without being read or scanned; that applies to every harness. The Cursor IDE reader still exports every pass. Tests count snapshots and scans, and each part was removed alone to confirm its test fails.
