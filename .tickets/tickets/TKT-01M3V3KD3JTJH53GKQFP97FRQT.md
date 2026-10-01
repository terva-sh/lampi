---
schema: 3
id: TKT-01M3V3KD3JTJH53GKQFP97FRQT
title: Count matching events by field in export and query
type: task
status: ready
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
updated_at: 2026-10-01T06:49:32Z
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

- [ ] --count-by on export, query events and count_by on the stream give the same counts for the same inputs.
- [ ] Free-text paths and --fields with --count-by are refused; docs list the refused paths.
