---
schema: 3
id: TKT-01M3K45MSX5SG7YZZCR399S9XF
title: "serve compact: fold grown chains into prefix records and drop leftovers"
type: task
status: ready
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
claim: null
archive: null
created_at: 2026-09-28T04:25:34Z
updated_at: 2026-09-28T04:25:34Z
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

- [ ] --dry-run reports folds, flattens and reclaimable bytes without writing
- [ ] Every fold and flatten is hash-verified in one pass per chain before an object is removed
- [ ] Unreferenced objects past a minimum age are removed
- [ ] A second run on a compacted lake changes nothing
