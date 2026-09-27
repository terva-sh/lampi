---
schema: 3
id: TKT-01M3F2RKGB79Y16RGTW3Z244QC
title: "Catalog: record idempotent accepted head-update history"
type: task
status: ready
status_reason: null
priority: normal
due_on: null
labels:
  - area/catalog
  - area/protocol
assignees: []
milestone: null
parent: TKT-01M3F2RKCZZNB6C1EGEG1FDCQH
origin: null
dependencies:
  - TKT-01M3F2FSTF28GNEDGQ0XBSZ44W
blocks_on: none
references:
  - ref: plan:web-ui
    path: docs/web-ui-plan.md
claim: null
archive: null
created_at: 2026-09-26T14:44:00Z
updated_at: 2026-09-27T22:08:39Z
created_by:
  id: agent:codex/web-ui-planning
  name: ""
updated_by:
  id: agent:claude-code/e226d0e4
  name: ""
extensions: {}
---

## Description

### Scope and rationale

Add prospective history for accepted session head changes in the same catalog transaction that records the change. Persist an independent coverage-start marker. Do not infer historic events from sessions.ingested_at or provenance. A retry that is unchanged or stale produces no new event. Record metadata only; no transcript content, secrets or auth identifiers.

### Where the head moves

Only `Catalog.IngestChanged` in internal/catalog/catalog.go writes `sessions.head_sha256`: the INSERT for a new session and the UPDATE when `newHead != head`. Merge, renormalize and publish never move it. Purge goes through `Catalog.DeleteSession`. `serve backup` copies the catalog with `VACUUM INTO`, so a new table and a `lake_meta` row are carried without backup changes; the work is to prove it.

### Contract

Follow docs/web-ui-plan.md release C. Receipt time is the `now` the lake passes to Ingest, stored as UTC Unix nanoseconds so the read side can bucket with integer division on an index. The coverage marker is a `lake_meta` row written by the migration with SQLite's clock, because migrations take no clock argument. A lake created by this binary covers its whole life. Use isolated synthetic data; this work needs no hosted endpoint or production credentials.

## Acceptance criteria

- [ ] Every committed initial/new head has one matching metadata history event; transaction failure produces neither.
- [ ] Unchanged/stale retries, provenance-only additions and divergent copies without a head change add no event; legitimate repeated digest transitions remain recordable.
- [ ] Old/new logical sizes and receipt times support net-change measures without claiming network or physical disk bytes.
- [ ] Migration starts prospective coverage explicitly and never invents events; backup/restore preserves coverage/history and purge removes the session history.
- [ ] Tests prove idempotency, rollback, append/replacement, snapshot rewrites back to an earlier digest, multi-machine behavior and that the history indexes serve time and harness range scans (EXPLAIN QUERY PLAN).

## Definition of done

- [ ] Validation evidence and rationale are recorded; measurement and recovery behavior are documented.

## Implementation plan

1. Append migration `migrateHeadUpdates` to `migrations` in internal/catalog/catalog.go: table `head_updates` (`update_id INTEGER PRIMARY KEY AUTOINCREMENT`, `session_uid`, `machine_id`, `harness`, `received_ns INTEGER`, `old_sha256` (empty for a new session), `new_sha256`, `old_size`, `new_size`, `relation`). No uniqueness on digests, so A→B→A is recorded twice. Indexes `(received_ns)`, `(harness, received_ns)`, `(session_uid)`. Insert `lake_meta('head_updates_since', strftime('%Y-%m-%dT%H:%M:%fZ','now'))`.
2. In `IngestChanged`, after the head-bearing artifacts are applied and before commit, when `!exists || newHead != head`: read old size (0 for a new session) and new size with the query already used for `ack.HeadSize`, and insert one row. The relation is the head artifact's decision relation (head, grown_from). The old size must be read before anything could change it; artifact rows keep their size, so reading by digest after applyArtifact is equivalent — verify in a test.
3. `DeleteSession` adds `head_updates` to the tables it clears.
4. Add `HeadUpdatesSince(ctx) (time.Time, bool, error)`, tolerating a read-only open of an older catalog the way `LakeID` does.
5. Tests in internal/catalog/head_updates_test.go: new session, grown_from, unchanged repost, stale repost, divergent copy, provenance-only second machine with the same digest, second machine that moves the head, snapshot kind rewrite and rewrite back, rollback on a forced failure, purge, migration from a version-7 file with sessions present (no events invented, marker set), `VacuumInto` copy keeps rows and marker, and EXPLAIN QUERY PLAN for the two range scans.
6. Update docs/architecture.md catalog section and the release C paragraph of docs/web-ui-plan.md (old and new size).
