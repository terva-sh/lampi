---
schema: 3
id: TKT-01M3KD7DKR2SJHEBPG3KQ04PP8
title: Client re-sends the last chunk of a file past 32 MiB at every sync
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/agent
  - area/server
assignees: []
milestone: null
parent: TKT-01M3K45MQBRESGG5HEZZSZ3FDQ
origin: null
dependencies:
  - TKT-01M3KC2DAAZSVA0XQ23XAAXSSE
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T07:03:49Z
updated_at: 2026-09-28T07:03:49Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

After TKT-01M3KC2DA the lake stores a growing file past the 32 MiB
object cap about once, but the client still sends such a file's last
chunk whole at every sync: up to 32 MiB per sync for a few KiB of
appended lines. Files under the cap send only the tail.

### Directions

- Let the client send a tail for a file past the cap. The lake grows
  the previous version's last chunk with the tail (splitting at the
  cap when the chunk fills), records the new version as the previous
  chunk list with that last chunk replaced, and folds the old last
  chunk into a record, as `FoldGrowth` does.
- A v0.1.3 lake answers an oversized tail with 400 (`Grow` rejects a
  prefix at the cap), not the 409 prefix mismatch that makes a client
  widen to the whole file. The client needs to know the lake accepts
  it, so add a capability to the hello response and send oversized
  tails only when it is there.
