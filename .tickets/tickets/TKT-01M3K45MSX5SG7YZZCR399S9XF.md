---
schema: 3
id: TKT-01M3K45MSX5SG7YZZCR399S9XF
title: "serve compact: fold grown chains into prefix records and drop leftovers"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/cas
  - area/server
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3K38AAK8Q9P3RB6G8GP9JZ9
blocks_on: none
references: []
claim:
  actor: agent:claude-code/e4a47e8c
  branch: ops/compact
  worktree: /home/sothr/.t3/worktrees/lampi/t3code-e4a47e8c
  commit: dc61a3f448f1eb1afaca65da10220d64e5fd1e23
  session: null
  claimed_at: 2026-09-28T04:48:50Z
  expires_at: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-09-28T04:48:50Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Add `terva-lampi serve compact` to shrink an existing lake. It is
idempotent: every run re-evaluates the whole store.

- **Fold:** every version in a grown_from chain that is still a full
  copy becomes a prefix record of the chain's newest version.
- **Flatten:** a prefix record that points at an intermediate version is
  rewritten to point at the newest.
- **Verify before any delete:** one sequential pass over the newest
  version checks every version's hash. SHA-256 state is cloned at each
  version length, so the whole chain costs one read of its newest
  version.
- **Unreferenced objects:** tails and concatenated chunks that no
  artifact, logical file or prefix record names are removed once they
  are older than a minimum age.
- **`--dry-run`:** reports what would be folded, flattened and removed,
  and the bytes reclaimed, without writing. It can run while serve
  runs. A real run takes the lake lock.

## Acceptance criteria

- [x] --dry-run reports folds, flattens and reclaimable bytes without writing
- [x] Every fold and flatten is hash-verified in one pass per chain before an object is removed
- [x] Unreferenced objects past a minimum age are removed
- [x] A second run on a compacted lake changes nothing

## Implementation plan

`Server.Compact` builds on the CAS primitives Fold, Terminal, Entries
and RemoveEntry, in four steps.

1. **Plan.** For each session and relpath with more than one version,
   sorted by size, read the longest once and hash it incrementally. Sum
   does not reset the state, so each older version whose digest matches
   that many leading bytes is planned against it. The rows left form a
   divergent branch, and the same step repeats on them.
2. **Bases.** Each planned version's base is its newest, followed
   through versions that are themselves planned and through existing
   records, to a file that stays whole. Folds then never chain, and one
   run reaches a fixed point.
3. **Apply.** Fold writes the record and removes the object. It refuses
   (ErrWouldLoop) a base that reads from the digest. A record already
   pointing at the base is left alone.
4. **Sweep.** Keep everything reachable from ReferencedDigests (artifact
   sha256 and grown_from, session heads, provenance, head_updates) plus
   every last manifest's digests, tails and chunks, through chunk lists
   and records. An unparseable index stops the run. Remove the rest once
   older than --min-age (default 1h). In a dry run, planned folds stand
   in for the records they would write, so the report matches a real
   run.

A dry run takes no lock. A real run takes lake.lock.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T04:48:50Z

### Alternatives rejected

- **Fold every version into the file's largest version only.** A
  divergent copy that is the largest would leave the whole main line
  unfolded. Branches are peeled off instead, each folding into its own
  newest.
- **Apply folds as found, one path at a time.** A digest can be both
  the newest at one path and an older version at another, as when a
  second session holds an earlier copy. Folding in path order then
  leaves two-link chains, which the next run flattens, so a single run
  would not be idempotent. Bases are resolved across the whole plan
  first.
- **Count only artifact rows as references.** Session heads,
  provenance, head_updates and the last manifests also name digests. A
  sweep that missed one would delete a blob the catalog can still
  point at, so every source is unioned.
