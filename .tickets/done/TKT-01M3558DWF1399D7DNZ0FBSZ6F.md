---
schema: 3
id: TKT-01M3558DWF1399D7DNZ0FBSZ6F
title: Codex CLI rollout JSONL adapter
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/adapter
  - phase/2-harness
assignees: []
milestone: phase-2
parent: TKT-01M3558DV5NHYZBFMPV1AVRNYQ
origin: null
dependencies:
  - TKT-01M3558DPVCN661600P3F9HMB0
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-22T18:15:12Z
updated_at: 2026-09-23T13:01:52Z
created_by:
  id: human:sothr
  name: Drew Short
updated_by:
  id: agent:cursor/7a25
  name: Cursor cloud agent
extensions: {}
---

## Description

Watch $CODEX_HOME/sessions/**/rollout-*.jsonl; do not confuse with history.jsonl.

## Acceptance criteria

- [x] Watches $CODEX_HOME/sessions/**/rollout-*.jsonl
- [x] history.jsonl is not ingested as a rollout

## Implementation plan

Add a Codex CLI peer on the same Harness surface as terva and Claude Code.

### On disk
CODEX_HOME wins. When it is unset the directory is ~/.codex (USERPROFILE\.codex on Windows), which is the Codex CLI default. Discovery and the watcher accept only sessions/**/rollout-*.jsonl. history.jsonl is not a rollout, including when it sits under sessions/. Other JSONL names and archived_sessions/ are out of this adapter.

### Record shape
The rollout line is internal and the reader version is pinned at 1, the same rule as the Claude adapter. Unrecognized keys are kept. Session id and cwd are taken from a session_meta payload when one is present; otherwise the file name is the native id. history.jsonl is never opened as a session.

### Wiring
agent discover, watch, and sync include this home next to terva and Claude. The allowlist and the terva-only projector are unchanged.

## Summary

internal/adapter/codex watches $CODEX_HOME/sessions/**/rollout-*.jsonl. When CODEX_HOME is unset the directory is ~/.codex (USERPROFILE\.codex on Windows). history.jsonl is not a rollout, including a copy under sessions/, and archived_sessions/ is not walked. The reader version is pinned at 1 and unrecognized keys stay on the record.

agent discover, watch, and sync include this home beside terva and Claude Code. A temp-dir test places history.jsonl next to a rollout, under sessions/, and at the Codex home, and both discover and the watcher skip it. upload.Sync of an allowlisted rollout does not create a catalog session for that history file.

The synchronous projector still implements terva only. Project linking, the async normalizer, and the terva hook polish are not in this change.
