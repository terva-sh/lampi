---
schema: 3
id: TKT-01M3M5VEQ5RCXZMG7R8CES5Q62
title: A subagent transcript can become a Claude session's head
type: bug
status: draft
status_reason: null
priority: high
due_on: null
labels:
  - area/server
  - area/normalize
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-28T14:14:11Z
updated_at: 2026-09-28T14:14:11Z
created_by:
  id: agent:claude-code/e4a47e8c
  name: ""
updated_by:
  id: agent:claude-code/e4a47e8c
  name: ""
extensions: {}
---

## Description

Searching the hosted lake for `TKT-01M396R1CX` found session
`0fc11135-…` only through tool events at 2026-09-25T01:15–01:18, positions
#1025–#1574. Those events are in a subagent transcript,
`<session>/subagents/agent-a6a2c6720657e762b.jsonl`. The main transcript
(27,043 events, which includes two assistant messages containing the ID)
is not in the normalized view or the search index.

### Cause

- The Claude adapter lists a session's files in walk order. The
  directory `<session>/` sorts before its sibling `<session>.jsonl`, so
  subagent transcripts come before the main transcript in the manifest.
- The lake takes the first `transcript_jsonl` in a manifest as its head
  (`catalog.transcriptIndex`). A subagent becomes the session head.
- `api.headArtifacts` normalizes the head plus the current artifacts
  under the head's directory. With a subagent as head, that is
  `<session>/subagents/`, so the main transcript is left out.
- Reproduced in a test: one manifest `[subagents/agent-a.jsonl,
  sid.jsonl]` makes the subagent the head, and `Project` returns only
  the subagent's events.
- A related case: a manifest carrying only a new subagent file (the
  main transcript unchanged, so left out of the manifest) makes that
  file the manifest's head. `reviseDecisions` relates it to the session
  head at another relpath as if the transcript had moved, and in the
  test it did not become a current artifact at all.

### Directions

- On the lake, pick the head among a manifest's transcripts by the
  shallowest relpath, not the first listed. That fixes deployed agents
  without an upgrade.
- Apply the "the head moved to another relpath" rule in
  `reviseDecisions` only to a candidate that is not deeper than the
  current head, so a new subagent file stays its own artifact.
- Repair existing sessions: move a head that sits under another
  current transcript's directory back to that transcript, then run
  `serve normalize --all`.
- Check the Conflicts page for subagent files recorded as
  `divergent_copy` of their session's transcript.
