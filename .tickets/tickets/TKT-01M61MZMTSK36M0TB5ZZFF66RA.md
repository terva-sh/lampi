---
schema: 3
id: TKT-01M61MZMTSK36M0TB5ZZFF66RA
title: "Search index: store size, call, model and token columns per event"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/049834bc-index
  branch: worktree-agent-adf00fbe30e1bc1b5
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-adf00fbe30e1bc1b5
  commit: 2439f8a0f31b8907b1905d5ac3ea5bd602ed46da
  session: null
  claimed_at: 2026-10-28T16:31:06Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:36:16Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-index
  name: Claude Code local agent
extensions: {}
---

## Description

The search index `docs` table (internal/recall/index.go:71) stores content truncated to `IndexContentMax` = 256 KiB and no size before truncation, so the largest tool blocks report as 256 KiB. It also stores neither `tool.call_id` nor the model id.

Add three columns:

- `content_bytes INTEGER`: the length of `content_text` before truncation, null when the event has none.
- `call_id TEXT`: pairs a call with its result for latency and orphan checks.
- `model_id TEXT`: lets usage be split by model.

Add them together because each one enters `sig()`, which rewrites every row the first time. Index `call_id` within a session if pairing needs it. Measure the one-time rewrite on the internal lake and record the time and WAL growth, given the index maintenance issues in TKT-01M4ET6T and TKT-01M4F4SE.

## Acceptance criteria

- [ ] docs rows carry content_bytes (size before truncation), call_id and model_id.
- [ ] The three columns land in one change so the signature rewrite happens once.
- [ ] The one-time rewrite time and WAL growth on the internal lake are recorded.
- [ ] docs rows carry input_tokens, output_tokens, cache_read_tokens and cache_write_tokens from the event's usage (null stays null, a reported zero stays 0), in the same version bump.

## Implementation plan

Schema version 5, rebuilt rather than migrated, like versions 2 to 4.

1. docs gains `content_bytes INTEGER`, `call_id TEXT` and `model_id TEXT`, placed before `content`. Content stays the last column, so reading a new column never walks the overflow pages of a large content value.
2. `docRow.fill` records `len(*ev.ContentText)` before truncation, `ev.Tool.CallID` and `ev.Model.ID`. `sig()` hashes all three. `docWriter` binds 15 values per row (200 rows is 3000 parameters, well under SQLite's limit). The batch byte accounting still counts the truncated content, which is what the statement carries.
3. Two partial indexes, both `WHERE call_id IS NOT NULL`, so they hold only tool rows and no search query can use them. searchSQL never names call_id, so its plans stay as they are.
   - `docs_call ON docs(session_uid, call_id)` pairs a call with its result.
   - `docs_call_time ON docs(event_type, recorded_ns)` drives per-tool aggregates over a time range.
4. Bump indexVersion to 5 and document it in the comment. A rebuild writes no FTS deletes. ALTER TABLE ADD COLUMN plus a sig change would delete and reinsert every row and leave every old row in the FTS segments until optimize.
5. Tests: the three columns are stored (content_bytes is the untruncated size and null without content; call_id and model_id are null when absent); a change to any of them rewrites the row; a version 4 file is rebuilt; the search plans do not use the new indexes; the pairing and time-range aggregate queries do.
6. Measure a full rebuild on a synthetic lake with tool call and result pairs at v4 and v5: time, file size, peak WAL. Record it in a note.
7. Docs: the "Search index" section of docs/web-dashboard.md names the stored per-event fields and the one-time rebuild.

Alternatives: an ALTER TABLE migration loses because the sig change rewrites every row anyway, and each rewrite is an FTS delete as well as an insert. A plain (tool_name, recorded_ns) index loses because searchSQL filters by tool_name and since, and the planner could pick it and then sort by id.
