---
schema: 3
id: TKT-01M61TGYNBMQT3F0N8E59EE8M8
title: "Cursor CLI adapter: export blob rowid to order compacted branches"
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T17:56:59Z
updated_at: 2026-10-29T00:51:55Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-cursor
  name: ""
extensions: {}
---

## Description

The Cursor CLI normalizer orders blobs from the lists in the protobuf root blobs (internal/normalize/cursorcliorder.go). After two or more compactions, older branches may share only the messages both kept, and no root says which came first. A probe of local stores found no field that orders roots: field 26 holds one value per store and field 8 never names another root. Since review 2902 on #220, those events carry `extra.order = "unestablished"` instead of a guessed order.

The adapter reads `SELECT id, data FROM blobs ORDER BY id` (internal/adapter/cursorcli/snapshot.go). The `blobs` table is a rowid table, and Cursor writes a new root each time the conversation changes, so root rowid order is likely chronological. Exporting each blob's rowid (for example as `seq`) would let the normalizer order the older branches by their roots' rowids.

### Costs to weigh

- **Re-upload.** The artifact bytes change for every Cursor CLI store, so each one uploads again once. The reader version pin moves too.
- **Older agents.** Agents that do not send `seq` keep today's behaviour, so the normalizer must accept both.
- **An unverified assumption.** Check against real compacted stores that root rowid order matches the growth of the roots' lists before relying on it.

## Acceptance criteria

- [x] Root rowid order is checked against real compacted stores and found chronological, or the ticket records that it is not.
- [x] The normalizer orders older branches by root rowid when the export carries it, and keeps extra.order unestablished when it does not.

## Implementation plan

### Approach

1. Verify on real stores first (AC1). Copy each Cursor CLI store.db and its WAL that the adapter's discovery would list into a scratch dir, open the copy, decode the roots the way cursorCLIOrder does, and compare rowid order with list growth: for every pair of roots where one list strictly extends the other, the shorter should have the lower rowid; the latest root should have the highest rowid.
2. Adapter (internal/adapter/cursorcli/snapshot.go): read `SELECT id, rowid, data FROM blobs ORDER BY id` and write the rowid as `seq` on each blob row, between `id` and `data`. A `WITHOUT ROWID` table makes that query fail; read it again without `seq` rather than failing the export. Bump the pinned reader `Version` from 1 to 2, as the Cursor IDE reader did when its export changed (#53).
3. Normalizer (internal/normalize/cursorcliorder.go): when every root blob carries an integer `seq`, give each listed blob a `born` = the least seq of a root that lists it, and use it as the tie-break for blobs outside the latest root (newer born first in the DFS, so it lands later). Order is then established and `unsure` is nil. When any root lacks a usable seq (version 1 exports from older agents), keep today's export-order guess and `extra.order = "unestablished"`. `seq` is parsed leniently from raw JSON, so a malformed value reads as absent instead of failing the document.
4. Docs: docs/harnesses.md (root ordering paragraph, Cursor CLI section with the version, the field, and the one-time re-upload); package doc comments.
5. Tests: adapter export carries rowid as seq and omits it for WITHOUT ROWID; normalizer orders older branches by seq both ways round against hash order, and keeps unestablished with a root missing seq or a non-integer seq.

### Alternatives considered

- **Order by message blob rowids instead of root rowids.** Lost: the probe found message blobs at a higher rowid than roots that list them (always the first list entry), so Cursor rewrites some message rows and their rowids move. Root rowids are what the probe validated.
- **Order the older branches by the latest seq of a root that lists the message, or by a full sort on seq.** Lost: the first root that lists a message is when it entered the conversation; the max would move a shared message forward when a later root re-lists it. A full sort would throw away the topological walk that keeps calls before results.
- **Use seq whenever some roots carry it.** Lost: a partial set means a document that is not what any adapter writes; mixing seq and export order would present a guess as established. All-or-nothing keeps the unestablished marker honest.
- **Fail the export on a WITHOUT ROWID blobs table (break the pin on purpose).** Lost: the only loss is ordering, which version 1 already lived without; stopping capture for that is worse. The fallback is two lines.
- **Keep Version at 1 because the field is additive.** Lost: the export bytes and meaning change, and the reader pin is how a lake tells the shapes apart; the IDE reader bumped for an additive change too.
- **Strict `*int64` for seq in the normalizer.** Lost: a non-integer seq would fail json.Unmarshal of the whole blobs array, turning a bad ordering hint into a normalize error for the session.

## Notes

**agent:claude-code/049834bc-cursor** at 2026-10-29T00:48:47Z

AC1 probe on this workstation's Cursor CLI stores (copies in a scratch dir, read-only; counts only).

- 12 stores (chats and ACP sessions, as the adapter's discovery lists them), 26,590 root blobs. No store's blobs table has an INTEGER PRIMARY KEY; all are plain rowid tables.
- In 12 of 12 stores, the root named by latestRootBlobId has the highest rowid of any root.
- Pairs of roots where one list strictly extends the other: 2,836,577. The shorter list had the lower rowid in 2,836,560. The 17 others all come from one store and one event: a root whose list is a strict prefix (more than 5 shorter) of the root just before it in rowid order. That is a truncation, as a rewind would write; it contradicts list growth but not write order.
- Breaks in rowid order (the next root does not extend the previous): 6 stores have 2 or more. 4 of those 6 agree with list growth throughout with no branch interleaving. One is the store with the truncation. In one store a branch resumed after another had started, twice (one interruption was a single root, one several), so branches can interleave in write order; the walk still keeps each branch whole.
- 1,176 and 5,664 roots (in the two stores checked in detail) hold a list identical to another root's, so roots change for reasons other than the list.
- 3,335 roots list a blob with a higher rowid than the root itself; in the three stores checked for this, every one of those was the first entry of the list. Cursor rewrites some message rows, so message rowids are not a safe order; root rowids are.

Conclusion: root rowid order is chronological as far as list growth can show, and the normalizer may rely on it.

**agent:claude-code/049834bc-cursor** at 2026-10-29T00:48:54Z

Correction to the AC1 note above: 1,176 and 5,664 are counts of distinct root lists that two or more roots share, not counts of roots. The conclusion is unchanged. Residual risks: a branch that resumes after another started (seen twice in one store) is placed whole after the branch that interrupted it, so a few of its later messages land after messages written before them; SQLite's VACUUM may renumber rowids of a table without INTEGER PRIMARY KEY, which keeps relative order in practice but is not promised.

**agent:claude-code/049834bc-cursor** at 2026-10-29T00:49:03Z

Supersedes the first residual risk in the previous note: the branch that resumed starts first (its first message is born at the earlier root), so it is placed whole and the branch that interrupted it follows all of it, including the resumed branch's later messages that were written after the interruption.

## Summary

The Cursor CLI export (reader Version 2) writes each blob's SQLite rowid as seq between id and data; a WITHOUT ROWID blobs table is exported without seq. The normalizer (cursorcliorder.go) places blobs outside the latest root by the least seq of a root that lists them when every root has an integer seq, so older branches follow in write order and unsure is nil; version 1 exports, or any root without a usable seq, keep the export-order guess and extra.order unestablished. AC1: 12 local stores, 26,590 roots; latest root has the max rowid in all 12; 2,836,560 of 2,836,577 extending root pairs agree with rowid order, the 17 from one rewind-like truncation. Docs: docs/harnesses.md. Tests: TestBlobSeqIsRowid (adapter), TestCursorCLIOrderBySeq (normalizer), both fail without the change; full just ci passes. Release cost: every Cursor CLI session re-uploads once in full (every row's bytes change, so every chunk is new), and the lake's harness_version for those sessions moves to 2; sessions already on the lake keep the unestablished marker until an upgraded agent uploads them again. Older lakes ignore seq.
