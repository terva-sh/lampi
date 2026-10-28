---
schema: 3
id: TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
title: "Normalize: tool error flag for OpenCode, Cursor and grokbot"
type: task
status: in-progress
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies: []
blocks_on: none
references: []
claim:
  actor: agent:claude-code/049834bc-norm
  branch: worktree-agent-ae1d2160e416e8e66
  worktree: /home/sothr/workspace/git.local.sothr.com/terva-sh/lampi/.claude/worktrees/agent-ae1d2160e416e8e66
  commit: 2439f8a0f31b8907b1905d5ac3ea5bd602ed46da
  session: null
  claimed_at: 2026-10-28T16:30:49Z
  expires_at: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:35:39Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc-norm
  name: Claude Code local agent
extensions: {}
---

## Description

`tool.is_error` is set by the Claude, Codex, Grok and terva normalizers only. OpenCode, Cursor IDE, Cursor CLI and grokbot leave it null on every result, so tool error rates and error-streak findings are blank for those harnesses.

For each of them, find whether the source records a tool failure (a status field, an error part, an exit code) and map it to `is_error`. Where the source records nothing, leave it null and say so in docs/harnesses.md, so null keeps meaning "not known", not "succeeded".

## Acceptance criteria

- [ ] Each of OpenCode, Cursor IDE, Cursor CLI and grokbot sets is_error where its source records a failure.
- [ ] Where a source records nothing, is_error stays null and docs/harnesses.md says so.

## Implementation plan

### Approach

Evidence for each source format comes from the normalizer, its tests and fixtures, the adapter, closed tickets, and for Cursor CLI the key names and value types of three local store.db files read read-only (no content read or recorded).

- OpenCode: already maps it. `toolPart` emits a result for `state.status` `completed` (is_error false) and `error` (is_error true, text from `state.error`), since #37. No change beyond a test that pins it.
- Cursor IDE: `toolFormerData.status` records the outcome. Map `completed` to false and `error` to true; any other value (`cancelled`, `loading`, missing) stays null, because a cancelled call did not fail and an unfinished one has no outcome. The flag applies to the result promoted from `toolFormerData.result` and to `toolResults` elements of the same call id. No local Cursor IDE database exists on this machine, so the status values come from the published Cursor bubble shape, not from a fixture; the docs say so.
- Cursor CLI: the projector does not promote content parts into tool events at all (cursorcli.go header). The local stores show `tool-result` parts with `toolCallId`, `toolName`, `result` (a string) and `experimental_content`, and no error field. is_error stays null; documented.
- grokbot: the transcript is `{role, message:{content:[{type,text}]}}`. A `tool` role line has only text parts, no call id and no status, and the projector skips it. is_error stays null; documented.
- docs/harnesses.md gets a section listing, per harness, where tool.name and tool.is_error on a result come from, and where they stay null.
