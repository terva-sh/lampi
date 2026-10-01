---
schema: 3
id: TKT-01M3NPFNMHYXK230CJMGQ4VVTM
title: "Search index: FTS segments hold 0.5 GiB over live size after rebuild"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/27b21f4b
  branch: search/pass-writes
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-27b21f4b
  commit: af10f1578763f89faedb517c6eb35733c02b4a04
  session: null
  claimed_at: 2026-10-01T07:40:04Z
  expires_at: null
archive: null
created_at: 2026-09-29T04:24:06Z
updated_at: 2026-10-01T09:59:14Z
created_by:
  id: agent:claude-code/65ab7244
  name: ""
updated_by:
  id: agent:claude-code/27b21f4b
  name: ""
extensions: {}
---

## Description

The live size of the dev lake's search index is well below what is on disk once the WAL is set aside (the WAL is TKT-01M3NPFNJA, Search index WAL keeps its peak size).

A `VACUUM INTO` copy taken at 2026-09-29 04:17 UTC, about 55 minutes after the index-version-4 rebuild began:

| | before | after `optimize` + `VACUUM` |
|---|---|---|
| file | 2.21 GiB | 1.67 GiB |
| `fts_data` | 1,588 MiB, 425,815 rows | 1,029 MiB, 269,628 rows |
| `docs` | 609.5 MiB | 609.5 MiB |

`optimize` took 34 s on this workstation. Event text is 424 MiB over 713,425 rows.

Row ids reached 746,531 for 713,425 rows, so about 33k rows had been replaced since the rebuild. Every pass that deletes a row sets `deleted`, and the next reclaim is then a forced merge of 2000 pages. The index may therefore work its way down to its live size by itself. It may also not: new small segments keep arriving from the syncs of five active sessions.

To do:
- Measure the file over a few hours on the dev lake, and check whether the forced merges converge.
- If they do not, run a merge to completion after a pass that wrote a large share of the index, such as a rebuild. That can be spread over passes as a forced merge, or done as one `optimize`. `optimize` costs a transaction the size of the FTS index, about 1 GiB of WAL here.
- Find what still replaces rows after TKT-01M3NENNN8. Compare signatures of one session across two generations.

## Acceptance criteria

- [x] The forced merges are measured, and the answer to whether they converge is recorded
- [x] Nothing still replaces rows at a sync, or what does is found
- [x] serve compact frees the entries of deleted rows in the search index, with a test that fails without it
- [ ] The internal lake's index is compacted and its size before and after recorded

## Implementation plan

1. Answer the to-dos with measurements (findings note): no rows are replaced at a sync; the forced merges do not converge and are expensive; the live overhead is dead entries from earlier deletes.
2. `serve compact` gains a search index step, `recall.OptimizeIndex`: an FTS5 `optimize`, an incremental vacuum and a truncating checkpoint, with serve stopped. `--dry-run` reports the index's size.
3. Tests: `TestOptimizeFreesTheEntriesOfRemovedRows` removes a session, then checks that `optimize` shrinks the segments by at least 4x and the file. The kept session stays searchable, the removed one is gone, and the WAL ends at 0. It fails without the `optimize` statement. `TestCompactDryRunsBesideServeAndOtherwiseNeedsTheLock` checks both compact outputs with and without an index.
4. Docs: the Compact section of `docs/vps-bringup.md` and the `docs/cli.md` row.
5. The per-transaction WAL peak from automerge is split out as TKT-01M3V7ZNT0TTQV363HZ0VJ738E (Search index: automerge rewrites old segments inside large transactions).
6. On the internal lake, run `serve compact` at the next deploy and record the index size before and after.

## Notes

**agent:claude-code/27b21f4b** at 2026-10-01T06:24:58Z

### Answered on the internal lake, 2026-10-01: the forced merges do not converge

Measured on a copy by `storage-v0.5.1-BKPCeV5d/measure.sh`, two days after the 09-29 measurement above:

| | fts_data | file |
|---|---|---|
| as copied | 2102 MiB | 2958 MiB |
| after `optimize` | 1263 MiB (-40%) | about 2120 MiB |

- **optimize** took 52 s.
- **Segment rows** went from 756,939 to 330,409.
- **Indexed text** is 516 MiB over 903,407 rows, and `docs` takes 751 MiB.
- **The file after optimize is computed**, as 2958 MiB minus 839 MiB. The script's Python ran `PRAGMA incremental_vacuum` for one step only, which frees one page. A local test confirmed that.

The dead share grew from about 35% on 09-29 to 40%, so the 2000-page forced merge after each pass does not bring the index down to its live size.

**Proposal:** run a full `optimize` when the index has been idle and deleted data has passed a threshold, plus an operator command to run it by hand. It needs the WAL fix (TKT-01M3NPFNJA) first, because one optimize writes a transaction the size of `fts_data` through the WAL.

**Layout measurement, for TKT-01M3K45MX:** the same text with `detail=column` takes 581 MiB of `fts_data` once optimized. Its search behaviour was not tested.

**agent:claude-code/27b21f4b** at 2026-10-01T07:35:32Z

### One pass wrote about 300 MiB of WAL on the live lake, 2026-10-01

v0.5.2 truncates the WAL at the end of each pass (TKT-01M3NPFNJA), so a pass's writes now show up as a transient WAL size. On the internal lake, about three minutes after the v0.5.2 start, the agent had uploaded two growing sessions. `search.db-wal` was then 311397872 bytes, and it was 0 again a few minutes later. `search.db` went from 3131387904 to 3118047232 bytes over the same span.

So one pass that re-indexed a few live sessions wrote about 300 MiB, while the sessions only grew by kilobytes. That is a clue for this ticket's third to-do item: either most of each session's rows are replaced every generation, or FTS5's merges inside the session's transaction rewrite large segments. A transient peak is acceptable disk use. The write volume per pass, every few minutes, is the cost worth finding.

**agent:claude-code/27b21f4b** at 2026-10-01T08:01:42Z

### Findings, 2026-10-01

All measurements are on this workstation, with throwaway probes that are not committed.

**1. No rows are replaced at a sync any more.** I normalized real transcripts at 90% and at 100% of their lines, with a different projection time and digest each time, and compared row signatures by position. 0 rows changed in five Claude Code sessions (2.5k to 33k rows each) and three Codex sessions. TKT-01M3NENNN8 fixed the last cause. This answers the third to-do.

**2. An index built cleanly has no merge overhead.** The probe indexed all 497 local transcripts (546k rows, 239 MiB of text) the way a rebuild does: one transaction per session, with a reclaim every 25 sessions and reclaims at the end until settled. The resulting file is 962 MiB, with a 592 MiB `fts_data` payload.
- Forced merges run to completion took 77 steps. They wrote 1289 MiB of WAL and left 590 MiB.
- `optimize` afterwards had nothing to do.

**3. The live index's overhead is dead entries from deletions.** On 2026-10-01 the live index's `fts_data` was 2102 MiB, and 1263 MiB after `optimize`. Every pass that replaced rows before TKT-01M3NENNN8 left delete entries, and so did the index-version-4 rebuild. FTS5 frees them only when the segments holding them merge together. Forced merges run only after a pass deletes rows, and since finding 1 that almost never happens. So the overhead stays: the forced merges will not converge by themselves. They are also expensive, because running them to completion rewrites the whole index.

**4. The ~300 MiB WAL peak on the live lake is one large session's transaction.** A pass that appends to growing sessions writes little. Three sessions gaining 30 rows each wrote 0.5-0.9 MiB of inserts, plus merges and vacuums under 8 MiB, over 25 passes. A new session indexed whole is the expensive case:

| new session text | WAL, automerge 4 (default) | automerge 8 | automerge 0 |
|---|---|---|---|
| 11 MiB | 113 MiB | 55 MiB | 53 MiB |
| 9 MiB | 154 MiB | 52 MiB | 77 MiB |
| 7 MiB | 115 MiB | 44 MiB | 34 MiB |

- **It is not page-cache spill.** Frames equal distinct pages, and cache sizes of 2, 32 and 128 MiB gave identical WALs.
- **It is automerge.** It rewrites existing segments inside the session's transaction: the 9 MiB session touched 39k pages and grew the file by only 10k.
- **On the live lake,** one or two new sessions of 10-20 MiB of text, uploaded from other machines, account for the peak.

### Approach

- **Reclaim the legacy dead space once, offline, with `optimize`.** `serve compact` is already the offline maintenance command operators run after an upgrade (TKT-01M3NPFNP7), so it gains a search index step: an FTS5 `optimize`, then an incremental vacuum, then a checkpoint. It runs only with serve stopped. On the live lake this should take `fts_data` from about 2.1 to 1.3 GiB, and it writes a WAL about the size of the FTS index once, about 1.3 GiB.
- **Leave reclaim's merges as they are.** Nothing is deleted at a sync, so no new dead space builds up. A purge or `normalize --stale` deletes rows and can be followed by another compact.
- **Automerge tuning** cuts the per-transaction peak to about a third. It also changes how much merging is left to passes, and the segment count that queries read. It needs its own measurement of total writes and query time, so it gets its own ticket.

Alternatives considered:
- **Automatic optimize in serve when dead space is high.** FTS5 gives no cheap measure of dead space. Tracking deleted rows would need new persistent state, and an `optimize` inside serve is a 1.3 GiB transaction that holds the index's write lock for about a minute. That is not worth it for a one-time backlog.
- **Forced merges on every reclaim.** They rewrote 1.3 GiB to save 2 MiB on a clean index.
- **`detail=column`.** This was measured at 581 MiB on the live data, but trigram substring search needs positions.

**agent:claude-code/27b21f4b** at 2026-10-01T09:59:14Z

Merged in #180 (e680473). Reviews 1715 and 1717 led to: a freelist-empty check that a one-step vacuum fails (522 pages left), a busy-checkpoint error when another process reads search.db, and a dry-run size that includes the WAL. Criterion 4 waits for a release carrying #180 to be deployed to the internal lake. That deploy should stop serve, run serve compact, record the search index size before and after, and then start serve again. The expected result is fts_data going from about 2.1 to 1.3 GiB.
