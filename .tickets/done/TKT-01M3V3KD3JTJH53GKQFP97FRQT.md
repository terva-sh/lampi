---
schema: 3
id: TKT-01M3V3KD3JTJH53GKQFP97FRQT
title: Count matching events by field in export and query
type: task
status: done
status_reason: null
priority: low
due_on: null
labels:
  - area/search
assignees: []
milestone: null
parent: TKT-01M3FPP3H592T31Y2M3N347CPB
origin: null
dependencies:
  - TKT-01M3V3J8VZKAZJJAR9VTMDGJGD
  - TKT-01M3V3JSQXA831MB02BRWW6NHE
  - TKT-01M3V3KCRA78QSQTY4SV0JVRAJ
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-01T06:49:32Z
updated_at: 2026-10-01T08:47:57Z
created_by:
  id: agent:claude-code/dae09bda
  name: ""
updated_by:
  id: agent:claude-code/dae09bda
  name: ""
extensions: {}
---

## Description

### Why

Many of the questions an agent asks before it decides something are counts,
for example "which tools do agents call, and how often" or "which harnesses
produce failed tool calls". Downloading every matching row to count them
locally wastes transfer, and it puts transcript text on a machine that only
needed numbers.

### Scope

`--count-by PATH`, on `export` and on `query events` (and `count_by` on the
read API stream), takes any path that `--fields` accepts. Instead of
events it writes one `{"value": V, "count": N}` row per distinct value,
sorted by count descending and then by value. It composes with every
filter. With `--count-by`, `--fields` is refused.

A path whose values are free text, such as `content_text`, is refused. A
count of free text would return the text itself, defeating the reason to
count. The refused paths are listed in the docs.

## Acceptance criteria

- [x] --count-by on export, query events and count_by on the stream give the same counts for the same inputs.
- [x] Free-text paths and --fields with --count-by are refused; docs list the refused paths.

## Implementation plan

recall.Counter (internal/recall/count.go) counts selected lines by the compact JSON value at one path, null when absent, capped at CountMaxValues (100,000) distinct values, and writes {"value":V,"count":N} rows by count descending then value text. NewCounter takes any --fields path except free text: content_text, content_ref, extra, extra.*. export --count-by feeds writeSelected's kept lines to it and writes the rows at the end; the stream's count_by passes counter.Add as Select's emit and sends the rows before the end line, whose rows is then the number of count rows; query events passes --count-by through as count_by. --fields with --count-by is refused everywhere.

## Notes

**agent:claude-code/dae09bda** at 2026-10-01T08:08:18Z

### Decisions
- **Refused paths:** content_text and content_ref (free text by definition), and extra and extra.* (a harness names and fills extra keys as it likes, so any of them can hold text). Counting session_id or event_id is allowed: high cardinality, but not transcript text, and the cap bounds it.
- **A cap, not unbounded memory.** 100,000 distinct values. Past it, export fails and the stream ends with complete=false, error too_many_values and no counts, rather than a partial count that looks whole.
- **Oversized lines (over 16 MiB) are left out of counts** in both export and the stream, matching whole-event output, so the two agree; the stream's end line still counts them under oversized.
- **Tie order is by the value's JSON text**, so "Read" sorts before null. Deterministic, which the parity tests need.

### Evidence
- recall TestCounter: refusals, null for missing and non-events, ordering, the cap (a known value still counts at the cap).
- cli TestQueryEventsMatchesExport: --count-by tool.name and event_type give identical output from export and from query events against a real lake (so the stream's count_by agrees too). Export and query refusals: free-text paths, --fields together, non-events formats.
- web TestReadEventsStream: count_by rows and end line; free text, with fields, and empty refused.
- `GOFLAGS=-mod=mod just ci` passes.

**agent:claude-code/dae09bda** at 2026-10-01T08:35:36Z

Review 1706: finding-1 (high) accepted, the counter also stops when its distinct values' JSON text totals more than CountMaxBytes (32 MiB), since lines up to 16 MiB make 100,000 values unbounded in bytes; it fails without partial counts, as at the value cap. finding-2 (low) accepted, the stream's end-line rows counts the count rows actually written.

## Summary

Landed in #179 (merge 41867cc). --count-by PATH on export and query events, and count_by on the event stream, write one {"value":V,"count":N} line per distinct value instead of events, by count then value. Free-text paths (content_text, content_ref, extra, extra.*) and --fields with --count-by are refused. A count stops at 100,000 distinct values or 32 MiB of them (review 1706) and then fails without partial counts.
