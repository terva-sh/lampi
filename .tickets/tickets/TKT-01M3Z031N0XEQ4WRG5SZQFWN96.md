---
schema: 3
id: TKT-01M3Z031N0XEQ4WRG5SZQFWN96
title: Normalize core ACP for Grok Build
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/2-harness
assignees: []
milestone: null
parent: null
origin: null
dependencies:
  - TKT-01M3Z02QJNXSYB4XQHPJGD9R59
blocks_on: none
references: []
claim:
  actor: agent:cursor/e15e
  branch: cursor/grok-acp-normalize-e15e
  worktree: /workspace
  commit: 60a139c4d275973f535e2e3682aa4d8e9879f267
  session: e15e
  claimed_at: 2026-10-02T19:35:24Z
  expires_at: null
archive: null
created_at: 2026-10-02T19:05:08Z
updated_at: 2026-10-02T19:47:23Z
created_by:
  id: agent:cursor/6699
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/e15e
  name: Cursor cloud agent
extensions: {}
---

## Description

Project a stored Grok Build `updates.jsonl` onto schema_version 1. `session_id` is `grok:` plus the native session UUID. `updates.jsonl` is the ACP event stream and the source of truth. `chat_history.jsonl` is not the transcript. `summary.json` is session metadata (cwd, title, model), not the event list.

This waits on the discover/watch/manifest ticket. It does not change Cursor projectors, and it does not upload full sidecars or promote usage.

## Acceptance criteria

- [x] A stored grok transcript_jsonl projects onto schema_version 1 events
- [x] session_id is grok: plus the native session UUID
- [x] chat_history.jsonl is not read as the transcript
- [x] summary.json is not projected as the event stream
- [x] Cursor projectors are unchanged

## Definition of done

- [x] api.Server.Project dispatches harness grok
- [x] A test covers one ACP updates.jsonl fixture
- [x] go test ./... is green

## Implementation plan

Follow the Claude JSONL projector. Add `normalize.Grok` and dispatch it from `api.Server.Project` for harness `grok`. `session_id` is `grok:` plus the native session UUID. `harness_version` stays the pinned reader `"1"`. The projector has no Confidence field.

### What becomes an event
`updates.jsonl` is the ACP stream. Consecutive `user_message_chunk`, `agent_message_chunk`, and `agent_thought_chunk` lines of the same kind coalesce into one `message`. A prompt marker change (`params._meta.promptId`, or `update._meta.promptIndex`) starts a new message. `tool_call` becomes `tool_call`. `tool_call_update` with status `completed` or `failed` becomes `tool_result`. Any other method or `sessionUpdate`, including xAI extensions and `usage_update`, is skipped and does not fail the file. A line that is not a JSON object follows the shared unreadable-line policy.

### What stays out
`projectKind` reads `transcript_jsonl` only. `summary.json` and `chat_history.jsonl` are not the event stream, including when a manifest labels them `transcript_jsonl`. Cursor and Cursor CLI projectors are not edited. Usage is not promoted.

### Tests
A unit fixture covers coalesced chunks, a tool call, a completed and a failed tool update, and unknown lines. An API smoke posts that fixture plus a summary and a chat history, checks `Project` writes `grok:<uuid>` at schema_version 1, and checks ShareGPT keeps the message and the tool. `PlantGrok` writes one ACP `user_message_chunk` so a planted session projects.

## Notes

**agent:cursor/e15e** at 2026-10-02T19:47:23Z

normalize.Grok projects updates.jsonl. Consecutive user_message_chunk, agent_message_chunk, and agent_thought_chunk lines coalesce into one message. tool_call becomes tool_call. tool_call_update completed or failed becomes tool_result. Other methods and sessionUpdate values are skipped. summary.json and chat_history.jsonl are not projected. harness_version stays 1 and the projector has no Confidence field. Cursor packages are unchanged.
