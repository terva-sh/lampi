---
schema: 3
id: TKT-01M3AQK47R1Z3T2PC6FH7N97VR
title: Cursor 1d promote composer usage and summary
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
created_at: 2026-09-24T22:11:49Z
updated_at: 2026-09-24T22:19:21Z
created_by:
  id: agent:cursor/d660
  name: Cursor cloud agent
updated_by:
  id: agent:cursor/d660
  name: Cursor cloud agent
extensions: {}
---

## Description

Promote composer-level usage and conversation summary in the Cursor IDE normalize projector only.

A `composerData:` object can carry `usageData` and `latestConversationSummary`. Today those fields stay on the composer meta event. This ticket makes a sibling `usage` event and a sibling `compaction` event. The Cursor adapter, its Version, and the cursor-cli projector stay as they are.

### Usage

`usageData` on a `composerData:` object becomes a sibling `event_type=usage` (`EventUsage`). The composer meta event remains.

`usage.cost_usd` is `costInCents / 100` when `costInCents` is present and numeric. Cents become USD. A missing cost leaves `cost_usd` null. A non-numeric `costInCents` is not a cost.

Token counts land on `Usage` (`input`, `output`, `cache_read`, `cache_write`) only when that object already has them under a recognizable token name. Cost, amount, and price are not token counts. Do not invent a count from those fields.

Unknown keys on `usageData` stay on the usage event's `extra`. The promoted object does not stay on the composer meta `extra`.

### Conversation summary

`latestConversationSummary` on a `composerData:` object becomes a sibling `event_type=compaction` (`EventCompaction`).

The human-readable text is `content_text` when the value is a string, or when the object has a string field `summary`. The rest of the object stays on the compaction event's `extra`. The promoted value does not stay on the composer meta `extra`.

### Out of scope

Bubble-level `tokenCount` and bubble-level `usageData` stay on the bubble event `extra`. They are not usage events. Soft-link, deploy, the Cursor CLI projector (`cursorcli`), AgentsView, PlantCursor, and a live Cursor install stay out. Operator docs stay out. There is no `Version` or `harness_version` bump.

## Acceptance criteria

- [x] A composerData usageData object becomes a sibling usage event. cost_usd is costInCents / 100 when that field is present and numeric. Recognizable token fields already on that object map to Usage input, output, cache_read, and cache_write. Unknown usageData keys stay on the usage event extra, not on the composer meta extra.
- [x] A missing cost leaves cost_usd null. A non-object usageData stays on the composer meta extra and is not a usage event. cost, amount, and price do not become token counts.
- [x] A composerData latestConversationSummary becomes a sibling compaction event. A string value, or an object field summary, is content_text. The rest stays on the compaction event extra, not on the composer meta extra.
- [x] Bubble usageData and tokenCount stay on the bubble event extra. They are not usage or compaction events.
- [x] go test ./internal/normalize/... passes. The Cursor adapter and its Version are unchanged.

## Definition of done

- [x] The projector change lives in internal/normalize/cursor.go and internal/normalize/cursor_test.go.
- [x] The Cursor adapter is unchanged, including Version. cursor-cli, soft-link, deploy, and AgentsView are unchanged. Operator docs are unchanged.

## Implementation plan

### Approach

Change `Cursor.projectComposerData` so one `composerData:` row returns the meta event plus siblings. `row` already accepts a slice from `projectBubble`. The meta event stays first.

A `usageData` object is a sibling `usage` event. `cost_usd` is `costInCents / 100` when that value is a JSON number. Token fields map only when the key is a recognizable token name and the value is a JSON integer: `input` / `inputTokens` / `input_tokens` / `promptTokens` / `prompt_tokens` for input, the same shape for output and completion, and cache-read / cache-write names that already say cache read or cache write. The first integer match for a slot wins. Later aliases stay in extra. `cost`, `amount`, `price`, `tokenCount`, and `costInCents` are not token counts. Leftover `usageData` keys, plus `composer_id` and `scope`, are the usage event extra. The object is removed from the meta extra. A non-object `usageData` stays on the meta extra and emits no usage event.

A `latestConversationSummary` string, or an object, is a sibling `compaction` event. `content_text` is the string, or the object's string field `summary` when that string is non-empty. Other object keys stay on the compaction extra with `composer_id` and `scope`. The value is removed from the meta extra. A number, array, or bool stays on the meta extra.

`Cursor.emit` is unchanged. `Usage` is set on the usage event after emit, the same way a tool call sets `Tool`. Bubble `usageData` and `tokenCount` stay on the bubble extra. `cursorcli` is untouched.

### Tests

`assertNoPromoted` still fails on a usage or compaction event whose raw type is not `composerData:`. Composer cases expect the sibling. Bubble cases still expect `usageData` and `tokenCount` on the bubble extra. Focused composer cases lock `costInCents` 250 to `cost_usd` 2.5, a missing cost, a non-object `usageData` left on meta extra, a string summary and an object summary, and an `amount` that does not become a token count.

## Notes

**agent:cursor/d660** at 2026-09-24T22:18:58Z

Operator docs still say composer usageData and latestConversationSummary stay on extra. That sentence is stale after this projector change. Quill owns the docs pass. This ticket does not edit docs/architecture.md or the README.

## Summary

Cursor.projectComposerData keeps the composer meta event and adds siblings. A usageData object is a usage event. cost_usd is costInCents / 100 when that value is a JSON number. Token counts are copied only from recognizable token names that are already JSON integers. cost, amount, price, and tokenCount are not counts. Leftover usageData keys sit on the usage event extra. A non-object usageData stays on the meta extra.

A latestConversationSummary string, or object, is a compaction event. content_text is the string, or the object's non-empty summary field. The other object keys sit on the compaction event extra. A composer title field named summary is not that event.

Bubble usageData and tokenCount stay on the bubble extra, including a bubble costInCents. The Cursor adapter Version is still 2. cursor-cli, soft-link, deploy, AgentsView, and operator docs are unchanged.

ShareGPT already treats compaction as a training turn, so the Cursor export fixture's summary text is no longer the composer title. The title stays off the training view. go test ./... passed.
