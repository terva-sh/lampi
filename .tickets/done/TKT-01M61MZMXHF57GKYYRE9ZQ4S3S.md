---
schema: 3
id: TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
title: "Normalize: tool error flag for OpenCode, Cursor and grokbot"
type: task
status: done
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
claim: null
archive: null
created_at: 2026-10-28T16:20:09Z
updated_at: 2026-10-28T16:42:32Z
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

- [x] Each of OpenCode, Cursor IDE, Cursor CLI and grokbot sets is_error where its source records a failure.
- [x] Where a source records nothing, is_error stays null and docs/harnesses.md says so.

## Implementation plan

### Approach

Evidence for each source format comes from the normalizer, its tests and fixtures, the adapter, closed tickets, and for Cursor CLI the key names and value types of three local store.db files read read-only (no content read or recorded).

- OpenCode: already maps it. `toolPart` emits a result for `state.status` `completed` (is_error false) and `error` (is_error true, text from `state.error`), since #37. No change beyond a test that pins it.
- Cursor IDE: `toolFormerData.status` records the outcome. Map `completed` to false and `error` to true; any other value (`cancelled`, `loading`, missing) stays null, because a cancelled call did not fail and an unfinished one has no outcome. The flag applies to the result promoted from `toolFormerData.result` and to `toolResults` elements of the same call id. No local Cursor IDE database exists on this machine, so the status values come from the published Cursor bubble shape, not from a fixture; the docs say so.
- Cursor CLI: the projector does not promote content parts into tool events at all (cursorcli.go header). The local stores show `tool-result` parts with `toolCallId`, `toolName`, `result` (a string) and `experimental_content`, and no error field. is_error stays null; documented.
- grokbot: the transcript is `{role, message:{content:[{type,text}]}}`. A `tool` role line has only text parts, no call id and no status, and the projector skips it. is_error stays null; documented.
- docs/harnesses.md gets a section listing, per harness, where tool.name and tool.is_error on a result come from, and where they stay null.

## Notes

**agent:claude-code/049834bc-norm** at 2026-10-28T16:42:05Z

### is_error, per harness: evidence and result

- opencode: already mapped before this ticket (opencode.go toolPart, since #37): `state.status` `completed` is false, `error` is true with text from `state.error`; `pending`/`running` emit no result. The ticket's premise was wrong for OpenCode. Added TestOpenCodeToolErrorFromState to pin the error case.
- cursor (IDE): mapped `toolFormerData.status`: `completed` false, `error` true, anything else (`cancelled`, `loading`, missing) null. Applies to the `toolFormerData.result` result and to a `toolResults` element of the bubble's own call id. Evidence is weaker than for the others: the fixtures in this repo carry no `status`, and this machine has no Cursor IDE state.vscdb. The values come from the published Cursor bubble shape. If a live export shows other spellings, cursorToolError in cursor.go is the one place to change.
- cursor-cli: the normalizer emits no tool results (content parts stay on an `unknown` event). Read the key names and value types, not content, of the three local `~/.cursor/chats/*/*/store.db` files, read-only: 40 `tool-result` parts, each with `type`, `toolCallId`, `toolName`, `result` (string) and `experimental_content` (list of text items); no `isError` or status field. Left null; documented.
- grokbot: transcript lines are `{role, message:{content:[{type,text}]}}`; `tool` role lines have text parts only, no call id or status, and the projector skips them (grokbot.go, grokbot_test.go, adapter/grokbot). Left null; documented.
- docs/harnesses.md "Tool names and errors on results" states for every harness where name and is_error come from and that null means not known.

## Summary

Cursor IDE results now take `tool.is_error` from `toolFormerData.status` (`completed` false, `error` true, anything else null), on both the `toolFormerData.result` result and a `toolResults` element of the bubble's own call. OpenCode already mapped `state.status` before this ticket; TestOpenCodeToolErrorFromState now pins the error case. Cursor CLI and grokbot record no tool failure and emit no tool results, so is_error stays null there. docs/harnesses.md has a new section, "Tool names and errors on results", with a table that says for every harness where a result's name and error flag come from, and that null means not known.

The Cursor IDE status values come from the published bubble shape. No fixture captured from a live Cursor IDE confirms them, because this machine has no Cursor IDE data. The docs and the note both say so. Tests: the "status sets is_error" subtest of TestCursorPromoteBubbleTools. `GOFLAGS=-mod=mod just ci` passes.
