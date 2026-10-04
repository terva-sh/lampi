---
schema: 3
id: TKT-01M44DDXWQ6MG51BMVF5FDCAEA
title: Cursor CLI normalize searches the whole export once per row
type: bug
status: done
status_reason: null
priority: high
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
created_at: 2026-10-04T21:34:28Z
updated_at: 2026-10-04T22:07:03Z
created_by:
  id: agent:claude-code/580cbe08
  name: ""
updated_by:
  id: agent:claude-code/580cbe08
  name: ""
extensions: {}
---

## Description

`normalize.CursorCLI` gives each meta row and blob an offset for its `content_ref` with `cursorOffset`, which runs `bytes.Index` over the whole export from the start for every row. The work is rows times document size. A real ACP export of 284 MB with about 20,000 blobs had not finished normalizing after 9 minutes; the lake's normalize worker sat in `cursorOffset` the whole time.

Before ACP sessions were read, Cursor CLI exports were small and this did not show. With TKT-01M44B45JRVQ1W2XF5H0K3MC4X (Cursor CLI: read ACP sessions under acp-sessions/), exports of hundreds of megabytes reach the lake, and the worker would spend its time on one session.

`cursorOffset` also matches the bare quoted key, so a row whose id an earlier row names as a field value got that earlier position.

The Cursor IDE projector in `internal/normalize/cursor.go` calls `cursorOffset` the same way. This ticket covers the CLI projector.

## Acceptance criteria

- [x] Normalizing a Cursor CLI export reads it about once, not once per row
- [x] A row's content_ref offset points at that row, not at an earlier row that names its id
- [x] A test fails if the offset search goes back to searching from the start

## Implementation plan

`CursorCLI.Normalize` keeps one `offsets` cursor over the export. Each row is found by its own field, `"key":"<key>"` for meta and `"id":"<id>"` for a blob, starting where the previous row was found. The offset still points at the quoted key, as before. A row that is not found that way, in a document written in another order or spacing, falls back to `cursorOffset` and does not move the cursor.

Alternatives considered:

- Record each row's offset while parsing the document with a streaming decoder. Lost: it means replacing `json.Unmarshal` of the whole export with a token walk for one field; the forward search costs one pass and changes nothing else.
- Keep searching for the bare quoted key, only forward. Lost: inside the previous row's data a field value equal to the next id matched first, which a test showed.
- Fix the IDE projector in the same change. Left out: its exports are per workspace and did not show the problem; noted on the ticket.

## Notes

**agent:claude-code/580cbe08** at 2026-10-04T21:34:28Z

Measured on a copy of a real 222 MB ACP store (exported as 284,062,434 bytes): normalize took 1.8 s and produced 13,451 events with the fix. Before it, the lake's worker was still in cursorOffset when the 9-minute test timeout fired. The new test fails when the forward search is disabled. The IDE projector (internal/normalize/cursor.go) still calls cursorOffset per row; not changed here.

**agent:claude-code/580cbe08** at 2026-10-04T21:39:03Z

Supersedes the plan's forward cursor. terva-review on #192 (review 2127, medium) found that a shared forward cursor can pass a row in a document with blobs before meta and then match a nested field after it, and only falls back when nothing later matches. Each row's span now comes from one json.Decoder pass over the meta and blobs arrays, and the row's field is looked for only inside its span; a row whose field is not found there gets its span start. Lengths that do not match the parse fall back to cursorOffset. The real 284 MB export normalizes in 2.8 s this way (1.8 s with the forward cursor). TestCursorCLIOffsetsStayInTheirRow covers reordered sections, a nested id, and a row whose own field is not found; unbounding the search or turning the spans off each fails a test.

**agent:claude-code/580cbe08** at 2026-10-04T21:43:44Z

Supersedes the previous note's in-span substring search. terva-review on #192 (review 2128, medium) found that inside one row a spaced top-level "id": "aaa" lets the compact nested "id":"aaa" in its data win. The decoder pass now walks each element's top-level keys and records where its own "key" or "id" value starts; there is no substring search. A row with no such field gets its first byte; a repeated field takes the last, as json.Unmarshal does for the parse. Real 284 MB export: 2.4 s, every row resolved. Mutations (offsets off, top-level field ignored) each fail a test.

**agent:claude-code/580cbe08** at 2026-10-04T21:55:09Z

terva-review on #192 (review 2129): a repeated null id or key moved the offset, though json.Unmarshal leaves the parsed string alone on null; fixed, with the case in TestCursorCLIOffsetsStayInTheirRow. The same review's finding about the 256 MiB cap and ACP support does not apply to this PR: those landed in #191, already on this PR's base (b2db016); this PR's diff is the normalize projector, its test, and tickets.

**agent:claude-code/580cbe08** at 2026-10-04T21:59:50Z

terva-review on #192 (review 2130): field names now match case-insensitively, as json.Unmarshal matches them for the parse; an ID-spelled blob is in the test. Four review rounds have each found a narrower way a hand-built export could diverge from the parse; the walker now follows encoding/json's matching for these fields (case, null, last repeat, top level only). The adapter writes compact, lowercase, single fields, so none of these cases arise from a real export.

## Summary

Landed in #192 (merge 90478f8). The Cursor CLI projector takes each row's content_ref offset from one json.Decoder pass over the meta and blobs arrays: the start of the row's own top-level key or id value, matched as encoding/json matches it for the parse (case ignored, last non-null repeat), or the row's first byte when it has none. No substring search over the document is left. A real 284 MB ACP export normalizes in 2.4 s; before, the lake's worker was still running at 9 minutes. Four terva-review findings were each fixed with a test (cross-row match after reordered sections, nested field in the same row, repeated null, field-name case); one finding about the 256 MiB cap was outside this PR (it landed in #191). The Cursor IDE projector has the same per-row search: TKT-01M44DRPVEDJ9H6Z31XMQ1Q1DF (draft).
