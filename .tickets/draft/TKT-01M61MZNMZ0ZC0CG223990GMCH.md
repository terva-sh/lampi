---
schema: 3
id: TKT-01M61MZNMZ0ZC0CG223990GMCH
title: Session diagnosis engine with coded findings and suggestions
type: task
status: draft
status_reason: null
priority: normal
due_on: null
labels:
  - area/server
assignees: []
milestone: null
parent: TKT-01M61MYZ6H6J8MGNPWHS8MTJ1X
origin: null
dependencies:
  - TKT-01M61MZN03D4QZ6CAJPRNDFM6S
  - TKT-01M61MZMR6EHF8BAC5F4VNK9VN
  - TKT-01M61MZMXHF57GKYYRE9ZQ4S3S
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-10-28T16:20:10Z
updated_at: 2026-10-28T16:20:10Z
created_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
updated_by:
  id: agent:claude-code/049834bc
  name: Claude Code local agent
extensions: {}
---

## Description

A streaming pass over one session's events file that returns findings, each with a stable code, a severity, the event positions involved, the measured values, and a suggestion.

Initial codes, with thresholds from the calibration spike: `large_tool_result`, `large_tool_input`, `error_streak`, `retry_loop` (same tool and input hash), `repeated_read` (needs a per-harness parser of each harness's tool inputs for the path), `context_pressure` (input token growth, compactions, cache-read share), `orphan_call`, `orphan_result`, `unreadable_lines`.

Suggestions are static text keyed by code, in an embedded file versioned like internal/advisory/agents.json, so they are reviewable and change with a release. Hash inputs; never return content. Memory stays bounded regardless of session size.

## Acceptance criteria

- [ ] One streaming pass returns findings with code, severity, positions, values and suggestion.
- [ ] The initial codes are implemented with thresholds from the calibration spike.
- [ ] Suggestions live in an embedded, versioned file keyed by code.
- [ ] Memory stays bounded on the large synthetic transcripts from the performance workloads.
