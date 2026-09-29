---
schema: 3
id: TKT-01M3PTMKG9Y2S51V5Q59YZ0P7D
title: "Conflicts: record resolutions, resolve TKT-01M3M5VEQ leftovers"
type: bug
status: done
status_reason: null
priority: high
due_on: null
labels:
  - area/catalog
  - area/server
assignees: []
milestone: null
parent: TKT-01M3PTMA4C1XD80THS0AXEKK1Y
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-29T14:55:56Z
updated_at: 2026-09-29T15:50:21Z
created_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/cd41c9ac
  name: Claude Code local agent
extensions: {}
---

## Description

All 184 `divergent_copy` rows on the dev lake are subagent transcripts
at `<session>/subagents/agent-*.jsonl`, written between 2026-09-26
18:58 and 2026-09-28 15:08 UTC, before TKT-01M3M5VEQ's fix. That bug
related a new subagent file with no current row at its path to the
session's own transcript, as if the transcript had moved. Every growth
of the file was stored again as a divergent copy. Migration 12 made the
newest copy current but left every row labelled `divergent_copy`, so the
Conflicts page and the overview count show 184 conflicts that are not.

### Approach

- A `conflict_resolutions` table keyed by artifact id: resolution,
  resolved_at, resolved_by, and a note. `relation` is not changed.
- Resolutions: `kept_head` (an operator kept the head),
  `made_head` (an operator made this copy the head), `superseded` (a
  later copy that extends this one was made the head) and
  `not_a_conflict` (the lake compared the copy with another file).
- Catalog calls to resolve and reopen, each queueing an audit event in
  the same transaction.
- A migration resolves the leftovers as `not_a_conflict`. A row
  qualifies when it is a companion of its session's head (under the
  directory named for the head's file) and no earlier artifact at its
  own session and relpath is anything but `divergent_copy`: it had
  nothing at its own path to diverge from, so it was compared with
  another file. A real divergence of a subagent file after the fix has
  an earlier head or grown_from row at its path and is left alone.
- The Conflicts page, the session's Conflicts tab, the overview count,
  `GET /v1/conflicts`, `GET /api/web/v1/conflicts` and
  `terva-lampi conflicts` show unresolved conflicts. Each takes a way
  to include resolved ones, and a resolved row names its resolution.

## Acceptance criteria

- [x] Resolving and reopening a conflict each queue an audit event in the same transaction
- [x] A migration resolves as not_a_conflict every divergent copy that is a companion of its session head with no earlier non-divergent row at its path
- [x] A real divergence of a companion file, after an earlier head or grown_from row at its path, stays unresolved
- [x] The Conflicts page, session Conflicts tab, overview count, /v1/conflicts, /api/web/v1/conflicts and terva-lampi conflicts show unresolved conflicts, and each can include resolved ones

## Implementation plan

Resolve, do not relabel. `relation` stays how the bytes compared, and
a new table records the decision.

- Migration 18, `migrateConflictResolutions`: `conflict_resolutions`
  (artifact_id PK, session_uid, resolution, resolved_at, resolved_by,
  note). It resolves as `not_a_conflict`, actor `catalog migration`,
  each divergent copy whose path is a companion of its session head and
  that has no earlier artifact at its session and path other than a
  divergent copy. Each is audited as `conflict.resolved`.
- `catalog.ResolveConflict` and `ReopenConflict` queue
  `conflict.resolved` / `conflict.reopened` in the same transaction.
  Errors: ErrNoConflict, ErrConflictResolved, ErrConflictOpen.
- `DivergentCopies(ctx, resolved)`, `PageRequest.Resolved`,
  `Record.Resolution`; the overview count is unresolved copies.
  `serve purge` deletes the session's resolutions.
- `GET /v1/conflicts?resolved=true`, `/api/web/v1/conflicts` and the
  session conflicts collection take `resolved=true`;
  `terva-lampi conflicts --resolved`. `api.WireConflicts` replaces the
  CLI's copy of the conversion.

### Alternatives

- Relabel leftovers as `grown_from`: claims a prefix relation nobody
  checked, and `compact`/`purge` walk grown_from chains. Rejected.
- Delete the leftover rows: loses the record, and the blobs are still
  referenced by provenance. Rejected; storage stays with compact/purge.
- Match leftovers by time (before the fix's deploy): the catalog has no
  migration timestamps, and a lake upgraded later would have a
  different cut-off. The structural rule (nothing at its own path to
  diverge from) is exact for the bug and needs no clock.

## Notes

**agent:claude-code/cd41c9ac** at 2026-09-29T15:03:17Z

Not run against the live dev lake's catalog: /var/lib/terva-lampi/catalog.db is not readable by this user. The migration test builds the three shapes (pre-fix leftovers, a post-fix divergence at the same path, a moved transcript); each rule was mutation-checked (dropping the no-earlier-row clause, the companion check, or the list filter fails the tests). Expected on the dev lake after upgrade: the 184 rows resolve as not_a_conflict and the Conflicts page is empty.

**agent:claude-code/cd41c9ac** at 2026-09-29T15:25:30Z

terva-review 1387 (run a9b15e2a) on 9260d95: two findings, both accepted and fixed. high: ResolveConflict accepted made_head/superseded, which would record a head change that did not happen; now it takes kept_head and not_a_conflict only, and only MakeConflictHead (TKT-01M3PTMWM) records the other two in the transaction that moves the head. low: GET /v1/conflicts?resolved=true&resolved=false read the first value; a repeated resolved is now 400. Tests cover both.

**agent:claude-code/cd41c9ac** at 2026-09-29T15:36:04Z

terva-review 1389 (run 9981626d) on 9adc1a0: prior two findings verified resolved. New high finding: the migration treats an earlier divergent_copy row as nothing to diverge from, even if it is current. Premise was wrong: migration 12 relabels the copy it makes current as relation='head' (headrepair.go:92,134), so the test's setup is the real state; the PR text 'left every row labelled divergent_copy' misled the reviewer and was corrected. Accepted as hardening anyway, since MakeConflictHead (TKT-01M3PTMWM) leaves a current divergent_copy row: an earlier row now counts when it is current, whatever its relation. Test adds that shape; mutation-checked.

**agent:claude-code/cd41c9ac** at 2026-09-29T15:42:23Z

terva-review 1391 (run 3e595b59) on 24ee0fc: prior finding verified resolved. New high: the migration matched any companion path of any harness, so a real move into the head's directory could be resolved. Accepted: the rule is now Claude sessions, transcript_jsonl, <head stem>/subagents/NAME (one level), the only shape TKT-01M3M5VEQ left. Tests add a nested non-subagents companion (stays open) and a terva session with the subagent shape (stays open); mutation-checked (companion() instead of subagentOf, and dropping the harness/kind filter, each fail).

## Summary

Merged as #149 (main 3a226cd). conflict_resolutions (migration 18) records kept_head / made_head / superseded / not_a_conflict per divergent copy without changing relation or deleting bytes; ResolveConflict takes kept_head and not_a_conflict only, ReopenConflict removes one, each audited in its transaction. The migration resolves as not_a_conflict each Claude transcript_jsonl at <head>/subagents/NAME with no earlier current or non-divergent row at its path. The overview count, Conflicts page and tab, /api/web/v1/conflicts, GET /v1/conflicts and terva-lampi conflicts list open conflicts, and resolved=true / --resolved adds the rest. Four terva-review rounds; findings on made_head via ResolveConflict, repeated query params, current divergent rows, and migration scope were all fixed. Not run against the live catalog (not readable by the agent user): the expected result after upgrading the dev lake is the 184 subagent rows resolved and an empty Conflicts page.
