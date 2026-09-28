---
schema: 3
id: TKT-01M3KD7DKR2SJHEBPG3KQ04PP8
title: Client re-sends the last chunk of a file past 32 MiB at every sync
type: task
status: done
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
updated_at: 2026-09-28T18:03:34Z
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

## Implementation plan

Let a file past the 32 MiB cap travel as a tail, to a lake that says it
can take one.

- `protocol.HelloResponse.Features`, with `FeatureLargeTails`
  ("large_tails"); the lake lists it.
- Lake: `checkClient` sends a tail whose file is past the cap to
  `growParts`. `cas.Store.GrowParts(prev, tail, limit)` reads prev's last
  piece (hash-checked) and the tail, writes the last piece extended up to
  the limit and the rest of the tail as a new piece, and supersedes the
  old last piece with a prefix record of its extension. `BindLogical`
  then binds the grown digest to the pieces and checks the whole hash;
  a mismatch is a prefix mismatch, and the client widens.
- Client: `prepare` builds a tail for any file whose tail fits in one
  blob. `sessionOverCap` widens only a tail past the cap sent to a lake
  without the feature, or a tail itself past the cap. A whole file past
  the cap no longer widens its session's other tails.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T17:51:14Z

### Alternatives considered

- Keep the protocol and have the lake infer a large tail. A lake from
  v0.1.3 answers such a manifest 400 (Grow refuses a prefix at the cap),
  not the 409 that makes a client widen, so a new agent would fail
  against an old lake. The hello feature lets the agent ask first.
- Bump `capture_protocol` to 2. It refuses every older agent or lake
  pairing for one optional behaviour; a feature list degrades instead.
- Avoid the whole-file hash in `BindLogical` (41 MiB read per sync for
  this session). The hash is the lake's only check that the pieces make
  the file the client named; reading local disk is cheaper than the
  9 MiB each sync used to send.

### Result

For a 37-byte append to a file 4 KiB past the cap, the agent sends 37
bytes to a lake with the feature and 4,133 to one without
(`TestSyncSendsOnlyTheTailOfAFilePastTheCap`). For this session's 41 MiB
transcript the second case was about 9 MiB per sync.

## Summary

Fixed in #81. The lake lists large_tails in hello's features and grows a file past the cap from a tail (cas.GrowParts extends the last piece up to the cap, adds the rest as a new piece, keeps the old last piece as a prefix record; BindLogical checks the whole hash). The agent sends such tails only to a lake that lists the feature. A 37-byte append past the cap now sends 37 bytes, not the last chunk (about 9 MiB per sync for a 41 MiB transcript). Needs the lake and agents upgraded, lake first.
