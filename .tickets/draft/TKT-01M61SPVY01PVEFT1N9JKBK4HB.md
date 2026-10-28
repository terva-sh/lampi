---
schema: 3
id: TKT-01M61SPVY01PVEFT1N9JKBK4HB
title: Project Claude Code's offered tool list for the context hint
type: task
status: draft
status_reason: null
priority: low
due_on: null
labels:
  - area/normalize
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T17:42:44Z
updated_at: 2026-10-28T17:42:44Z
created_by:
  id: agent:claude-code/049834bc-stats
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-stats
  name: Claude Code local agent
extensions: {}
---

## Description

The starting-context hint (TKT-01M61MZNW) can only correlate starting context with the servers a session called, because no normalized event records which tools a session was offered. Claude Code does record this. Recent transcripts carry `attachment` lines of type `deferred_tools_delta`, with `addedNames`, `removedNames` and MCP server status lists such as `pendingMcpServers` and `failedMcpServers`. They also carry `prompt_snapshot` attachments with a `tools` key. The Claude normalizer projects both as unknown events with the payload in `extra`, which the search index does not store.

Codex rollouts record no tool list. `session_meta` and `turn_context` carry no tool or server fields; this was checked on recent rollouts, by key names only.

Projecting the offered tool names would let the hint report each server's offered-tool count per session directly, and would show servers that were configured but never called, which the correlation cannot see. Decide how the names reach the stats engine: an indexed column, a per-session summary row, or a read of the events file. Check the attachment shapes against Claude Code's current transcripts before relying on them, since attachment types change between releases.
