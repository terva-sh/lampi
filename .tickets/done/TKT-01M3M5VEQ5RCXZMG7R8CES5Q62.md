---
schema: 3
id: TKT-01M3M5VEQ5RCXZMG7R8CES5Q62
title: A subagent transcript can become a Claude session's head
type: bug
status: done
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
updated_at: 2026-09-28T16:14:27Z
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

## Implementation plan

Fix it on the lake, so deployed agents need no upgrade, and repair the
catalog in a migration. A companion of a file is one inside the
directory named for it: `<sid>/subagents/x.jsonl` of `<sid>.jsonl`.

- `catalog.HeadIndex` (was `transcriptIndex`, duplicated as
  `api.headIndex`): a manifest's head is its first transcript that is
  not a companion of another transcript in the manifest.
- `reviseDecisions`: a companion of the session head never moves the
  head, and the "file moved" rule skips companions in both directions.
  A manifest carrying only a new subagent file then records it as its
  own current artifact.
- Migration 12, `migrateSubagentHeads`: a head that is a companion of
  another current artifact of its kind moves back to that artifact; the
  newest divergent copy that is a companion of the head, at a relpath
  with no current row, becomes current; each changed session is queued
  for normalization.

## Notes

**agent:claude-code/e4a47e8c** at 2026-09-28T15:10:52Z

### Alternatives considered

- Sort artifacts in the Claude adapter so the session's transcript
  comes first. It fixes new manifests from upgraded agents only, and a
  sync that carries only a subagent file still names it as the head.
- Choose the head at normalization time (`headArtifacts`) instead of
  at ingest. It fixes search and events, but `head_sha256` stays wrong
  for the dashboard, activity sizes, and the next manifest's relation.
- Repair from `serve normalize --all` instead of a migration. It would
  need the operator to know to run it; the migration runs on the first
  start and queues only the sessions it changed.
- Keep the "file moved" rule for any depth no deeper than the head.
  A session whose head was a subagent would then take its own transcript
  for a move of that subagent and store it as a divergent copy.

**agent:claude-code/e4a47e8c** at 2026-09-28T15:12:44Z

### Correction to the first approach

The first version chose the shallowest transcript and refused any head
deeper than the current one. CI found two cases it broke: an OpenCode
export that moved from `export/ses_1.json` to
`export/<date>/ses_1.json` is a real move one level deeper, and a Codex
manifest's `history.jsonl` sits above its rollout but is not the
session. The rule now is the directory named for the file: a companion
of `<sid>.jsonl` is anything under `<sid>/`, which is exactly where
Claude Code keeps subagent transcripts.

## Summary

Fixed on the lake in #64. A manifest's head is its first transcript that is not inside the directory named for another (<sid>/ of <sid>.jsonl), and such a companion never moves the session head. Migration 12 (migrateSubagentHeads) moves existing subagent heads back to the session's transcript, current or kept as a divergent copy, makes companion divergent copies current, and queues each changed session for normalization, so no manual --all is needed. Verify on the hosted lake: search TKT-01M396R1CX should find session 0fc11135's assistant messages from 09-24/25.
