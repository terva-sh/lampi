---
schema: 3
id: TKT-01M3AH7GRNF5N1DWBF41DM1GH6
title: Cursor 1c normalize-only tool promote
type: task
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/normalize
  - phase/4-cursor
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-24T20:20:37Z
updated_at: 2026-09-24T20:29:06Z
created_by:
  id: agent:cursor/b362
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/b362
  name: Cursor cloud agent
extensions: {}
---

## Description

Promote Cursor IDE bubble tools in the normalize projector only.

A `bubbleId` object can carry `toolFormerData` and `toolResults`. Today those objects stay on the parent event. This ticket makes a sibling `tool_call` and `tool_result` when the bubble has a string tool name and a string call id. The Cursor adapter, its Version, and the cursor-cli projector stay as they are.

### Call

`toolFormerData` promotes when `name` is a non-empty string and a call id is a non-empty string. The call id is `toolCallId`, otherwise `id`. `capabilityType`, `capabilities*`, and a numeric `tool` field do not promote on their own.

`content_text` is `rawArgs` when that value is a non-empty JSON string, otherwise `params` on the same rule, otherwise the JSON of `args`.

### Result

A `tool_result` uses `toolFormerData.result` when that value is a string. Otherwise each `toolResults` element that has a call id and a result string becomes one `tool_result`. An empty `toolResults` array does not block `toolFormerData.result`. The result call id is the one on that object (`toolCallId`, otherwise `id`).

### Bubble group

A type 1 or type 2 bubble keeps its message when the visible text is non-empty. A tool-only bubble whose text is empty emits the tool events and no empty message. Inside one bubble the order is message, then `tool_call`, then `tool_result`. `fullConversationHeadersOnly` still reorders bubbles, and a move carries the whole bubble group.

A bubble that is missing the name or the call id is not promoted. The object stays on `extra`.

`usageData`, `tokenCount`, and `latestConversationSummary` stay on `extra`. They are not usage or compaction events.

## Acceptance criteria

- [x] A bubble toolFormerData with a string name and a string call id (toolCallId, otherwise id) becomes a sibling tool_call. content_text is rawArgs or params when that value is a non-empty JSON string, otherwise the JSON of args. A live-shaped fixture locks toolCallId, a rawArgs string, and result on toolFormerData with empty toolResults.
- [x] A tool_result uses toolFormerData.result when that value is a string. Otherwise each toolResults element with a call id and a result string becomes one tool_result. Empty toolResults does not block the result string.
- [x] A type 1 or 2 message is kept when its text is non-empty. A tool-only bubble with empty text emits tools and no empty message. Order inside the bubble is message, tool_call, tool_result. A header reorder moves the whole bubble group.
- [x] Missing name or call id stays on extra and is not promoted. capabilityType, capabilities, and a numeric tool field do not promote by themselves. usageData, tokenCount, and latestConversationSummary are not usage or compaction events.
- [x] go test ./internal/normalize/... passes. The Cursor adapter and its Version are unchanged.

## Definition of done

- [x] The projector change lives in internal/normalize/cursor.go and internal/normalize/cursor_test.go.
- [x] The Cursor adapter is unchanged, including Version. cursor-cli, soft-link, deploy, and AgentsView are unchanged.

## Implementation plan

### Approach

Change `Cursor.projectBubble` so one `bubbleId` row can emit a message plus tool siblings. `row` returns a slice. `orderCursorBubbles` already sorts by `bubble_id` with a stable sort, so siblings that share `composer_id` and `bubble_id` move as one group when headers reorder.

A tool call requires a string `name` and a string call id. The call id is `toolCallId`, then `id`. Args text is a non-empty `rawArgs` string, then a non-empty `params` string, then the JSON of `args`. A result string on `toolFormerData` wins over `toolResults`. An empty `toolResults` array is not a result list. Elements missing a call id or a result string stay in `extra` only.

Type 1 and type 2 still emit a message when the visible text is non-empty, and when the text is empty and nothing was promoted. Empty text with a promoted tool emits tools only. `usageData`, `tokenCount`, and `latestConversationSummary` stay on `extra`.

### Tests

`assertNoPromoted` allows `tool_call` and `tool_result` and still fails on `usage` and `compaction`. A live-shaped fixture locks `toolCallId`, a `rawArgs` string, `toolFormerData.result`, and empty `toolResults`, including the empty-text tool-only path. Fallback fixtures lock `id`, object `args`, `params`, and `toolResults`. Malformed bubbles and capability-only bubbles stay unpromoted. A header fixture checks message, tool_call, tool_result order and that the whole group moves.

## Notes

**agent:cursor/b362** at 2026-09-24T20:29:06Z

QE matrix cases are named subtests of TestCursorPromoteBubbleTools: empty text emits tools only; malformed toolFormerData stays on extra; usage tokenCount and summary stay unpromoted. assertNoPromoted still allows tool_call and tool_result and fails on usage and compaction.

## Summary

Cursor.projectBubble promotes a bubble toolFormerData into a sibling tool_call when name is a non-empty string and the call id is a non-empty string. The call id is toolCallId, then id. content_text is a non-empty rawArgs JSON string, then params on the same rule, then the JSON of args. A tool_result uses toolFormerData.result when that value is a string, including when toolResults is empty. Otherwise each toolResults element with a call id and a result string is one tool_result.

A type 1 or 2 message stays when its text is non-empty. Empty text with a promoted tool emits tools only. Order inside the bubble is message, tool_call, tool_result. fullConversationHeadersOnly moves that whole group. A missing name or call id, and a capabilityType or numeric tool field alone, stay on extra. usageData, tokenCount, and latestConversationSummary are not usage or compaction events.

The Cursor adapter and Version are unchanged. cursor-cli, soft-link, deploy, and AgentsView are unchanged. go test ./... passed. The export fixture that already carried name, id, and args now expects that tool_call in the ShareGPT row.
